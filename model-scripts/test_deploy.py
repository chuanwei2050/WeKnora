#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""model-scripts 离线单元/冒烟测试（不拉模型、不构建镜像）。"""

from __future__ import annotations

import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))

import deploy  # noqa: E402


class TestChecksums(unittest.TestCase):
    def test_write_and_verify(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "a.bin").write_bytes(b"abc")
            (root / "sub").mkdir()
            (root / "sub" / "b.txt").write_text("hi", encoding="utf-8")
            deploy.write_checksums(root, algo="sha256")
            ok, errors = deploy.verify_checksums(root, algo="sha256")
            self.assertEqual(errors, [])
            self.assertEqual(ok, 2)

    def test_empty_checksum_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "a.bin").write_bytes(b"x")
            (root / ".checksums.sha256").write_text("", encoding="utf-8")
            ok, errors = deploy.verify_checksums(root, algo="sha256")
            self.assertEqual(ok, 0)
            self.assertTrue(any("空" in e for e in errors))

    def test_mismatch_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "a.bin").write_bytes(b"abc")
            deploy.write_checksums(root)
            (root / "a.bin").write_bytes(b"changed")
            ok, errors = deploy.verify_checksums(root)
            self.assertEqual(ok, 0)
            self.assertTrue(any("不匹配" in e for e in errors))


class TestConfigAndCompose(unittest.TestCase):
    def test_default_loads_local_config_yaml(self) -> None:
        path = deploy.resolve_config_path(None)
        self.assertIsNotNone(path)
        self.assertTrue(path.name == "config.yaml" or not (ROOT / "config.yaml").exists())
        cfg = deploy.load_config(path)
        self.assertIn("models", cfg)
        self.assertNotIn("vlm", cfg["models"])
        self.assertIn("chat", cfg["models"])
        self.assertEqual(cfg["models"]["chat"].get("roles"), ["vlm"])

    def test_embedding_compose_has_pooling_runner(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml", prefer_as_base=True)
        specs = deploy.parse_models(cfg)
        text = deploy.render_compose(cfg, Path("/mnt/models"), specs)
        import yaml

        yaml.safe_load(text)
        self.assertIn("--runner", text)
        self.assertIn("pooling", text)
        self.assertIn("weknora-rerank:airgap", text)
        self.assertIn("weknora-asr:airgap", text)
        self.assertIn("weknora-tts:airgap", text)

    def test_chat_vlm_shared_registry(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml", prefer_as_base=True)
        cfg["models"]["chat"]["enabled"] = True
        specs = deploy.parse_models(cfg)
        reg = deploy.render_approved_endpoints(cfg, specs)
        roles = {m["role"] for m in reg["model_registry"]}
        self.assertIn("chat", roles)
        self.assertIn("vlm", roles)
        chat_ep = next(e for e in reg["approved_endpoints"] if e["id"] == "model-chat")
        self.assertEqual(set(chat_ep["allowed_model_roles"]), {"chat", "vlm"})
        self.assertEqual(chat_ep["port"], 8000)

    def test_limit_mm_dict_to_json(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml", prefer_as_base=True)
        cfg["models"]["chat"]["enabled"] = True
        cfg["models"]["chat"]["limit_mm_per_prompt"] = {"image": 5}
        specs = deploy.parse_models(cfg)
        chat = next(s for s in specs if s.key == "chat")
        self.assertEqual(chat.limit_mm_per_prompt, '{"image":5}')
        text = deploy.render_compose(cfg, Path("/mnt/models"), specs)
        import yaml

        data = yaml.safe_load(text)
        cmd = data["services"]["chat"]["command"]
        idx = cmd.index("--limit-mm-per-prompt")
        self.assertEqual(json.loads(cmd[idx + 1]), {"image": 5})

    def test_quant_requires_download_id(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml", prefer_as_base=True)
        cfg["models"]["chat"]["enabled"] = True
        cfg["models"]["chat"]["download_id"] = None
        specs = [s for s in deploy.parse_models(cfg) if s.enabled]
        with self.assertRaises(RuntimeError):
            deploy.validate_specs_for_prepare(specs)

    def test_invalid_port_rejected(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml", prefer_as_base=True)
        cfg["models"]["embedding"]["port"] = 0
        with self.assertRaises(RuntimeError):
            deploy.parse_models(cfg)


class TestCli(unittest.TestCase):
    def test_config_before_subcommand(self) -> None:
        # --config 在子命令前：由 main() 预处理注入
        code = deploy.main(
            [
                "--config",
                str(ROOT / "config.yaml"),
                "gen-config",
                "--output-dir",
                str(ROOT / "generated"),
                "--host",
                "10.0.0.9",
            ]
        )
        self.assertEqual(code, 0)
        data = json.loads((ROOT / "generated" / "approved_endpoints.json").read_text(encoding="utf-8"))
        self.assertEqual(data["meta"]["host"], "10.0.0.9")

    def test_config_after_subcommand(self) -> None:
        parser = deploy.build_parser()
        args = parser.parse_args(
            ["prepare", "--config", str(ROOT / "config.yaml.example"), "--skip-docker", "--no-archive"]
        )
        self.assertEqual(Path(args.config).name, "config.yaml.example")
        self.assertTrue(args.skip_docker)

    def test_airgap_blocks_prepare(self) -> None:
        import os

        os.environ["AIR_GAPPED_MODE"] = "true"
        try:
            with self.assertRaises(RuntimeError):
                deploy.require_online("prepare")
        finally:
            os.environ.pop("AIR_GAPPED_MODE", None)

    def test_gen_config_cli(self) -> None:
        out = ROOT / "generated"
        if out.exists():
            shutil.rmtree(out)
        code = deploy.main(
            [
                "gen-config",
                "--config",
                str(ROOT / "config.yaml"),
                "--output-dir",
                str(out),
                "--host",
                "10.0.0.8",
                "--data-dir",
                "/mnt/models",
            ]
        )
        self.assertEqual(code, 0)
        compose = out / "docker-compose.airgap.override.yml"
        self.assertTrue(compose.exists())
        text = compose.read_text(encoding="utf-8")
        self.assertIn("pooling", text)
        self.assertIn("10.0.0.8", (out / "approved_endpoints.json").read_text(encoding="utf-8"))


class TestSafeExtract(unittest.TestCase):
    def test_tar_path_traversal_rejected(self) -> None:
        import tarfile

        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            evil = tmp_path / "evil.tar.gz"
            with tarfile.open(evil, "w:gz") as tar:
                info = tarfile.TarInfo(name="../escape.txt")
                data = b"x"
                info.size = len(data)
                import io

                tar.addfile(info, io.BytesIO(data))
            dest = tmp_path / "out"
            dest.mkdir()
            with self.assertRaises(RuntimeError):
                deploy.safe_extract_tar(evil, dest)


class TestYamlQuote(unittest.TestCase):
    def test_json_arg_roundtrip(self) -> None:
        import yaml

        raw = '{"image": 5}'
        doc = yaml.safe_load(f"cmd:\n  - {deploy.yaml_quote(raw)}\n")
        self.assertEqual(doc["cmd"][0], raw)


if __name__ == "__main__":
    unittest.main(verbosity=2)
