#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""model-scripts 离线单元/冒烟测试（不拉模型、不构建镜像）。"""

from __future__ import annotations

import json
import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

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
        self.assertIn('device_ids: ["0"]', text)
        self.assertIn("--api-key", text)
        self.assertIn("MODEL_API_KEY must be set", text)
        self.assertIn("http://127.0.0.1:8000/health", text)
        self.assertNotIn("http://127.0.0.1:8000/v1/models", text)

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

    def test_verifier_awq_and_shared_judge(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml.example", prefer_as_base=True)
        self.assertIn("verifier", cfg["models"])
        specs = deploy.parse_models(cfg)
        verifier = next(s for s in specs if s.key == "verifier")
        self.assertTrue(verifier.enabled)
        self.assertEqual(verifier.quant, "awq")
        self.assertEqual(verifier.download_id, "tclf90/Qwen3.5-9B-AWQ")
        self.assertEqual(verifier.hub_id_hf, "QuantTrio/Qwen3.5-9B-AWQ")
        self.assertEqual(verifier.port, 8003)
        self.assertIn("evaluation_judge", verifier.all_roles)
        deploy.validate_specs_for_prepare([s for s in specs if s.enabled])
        reg = deploy.render_approved_endpoints(cfg, specs)
        roles = {m["role"] for m in reg["model_registry"]}
        self.assertIn("verifier", roles)
        self.assertIn("evaluation_judge", roles)
        ep = next(e for e in reg["approved_endpoints"] if e["id"] == "model-verifier")
        self.assertEqual(ep["port"], 8003)
        text = deploy.render_compose(cfg, Path("/mnt/models"), specs)
        self.assertIn("verifier:", text)
        self.assertIn("8003:8000", text)
        self.assertIn("reasoning-parser", text)

    def test_prefer_quantized_variants(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml.example", prefer_as_base=True)
        specs = {s.key: s for s in deploy.parse_models(cfg)}
        self.assertTrue(specs["embedding"].quant.startswith("awq"))
        self.assertIn("AWQ", specs["embedding"].hub_id.upper())
        self.assertTrue(specs["rerank"].quant.startswith("onnx"))
        self.assertIn("ONNX", specs["rerank"].hub_id.upper())
        self.assertTrue(specs["asr"].quant.startswith("onnx"))
        self.assertIn("onnx", specs["asr"].hub_id.lower())
        self.assertEqual(specs["tts"].quant, "fp16")
        text = deploy.render_compose(cfg, Path("/mnt/models"), list(specs.values()))
        self.assertIn("--quantization", text)
        self.assertIn("awq", text)
        self.assertIn("QUANT=onnx-int8", text)
        self.assertIn("QUANT=fp16", text)

    def test_asr_runtime_sidecar_is_configured(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml.example", prefer_as_base=True)
        asr = next(s for s in deploy.parse_models(cfg) if s.key == "asr")
        self.assertEqual(asr.extra_files[0]["model_id"], "iic/SenseVoiceSmall")
        self.assertEqual(asr.extra_files[0]["path"], "chn_jpn_yue_eng_ko_spectok.bpe.model")

    def test_runtime_sidecar_path_cannot_escape_model_dir(self) -> None:
        spec = deploy.ModelSpec(
            key="test",
            enabled=True,
            role="test",
            model_id="test/model",
            quant="fp16",
            engine="container",
            port=8000,
            served_model_name="test-model",
            extra_files=[{"path": "../escape.bin"}],
        )
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(RuntimeError):
                deploy.download_extra_files(spec, Path(tmp), source="auto", retries=1, delay=0)

    def test_runtime_sidecar_tokens_are_scoped_to_their_hub(self) -> None:
        with mock.patch.dict(
            os.environ,
            {"MODELSCOPE_TOKEN": "ms-secret", "HF_TOKEN": "hf-secret"},
            clear=False,
        ):
            self.assertEqual(
                deploy.extra_file_auth_headers("https://www.modelscope.cn/models/a/b"),
                {"Authorization": "Bearer ms-secret"},
            )
            self.assertEqual(
                deploy.extra_file_auth_headers("https://huggingface.co/a/b"),
                {"Authorization": "Bearer hf-secret"},
            )
            self.assertEqual(
                deploy.extra_file_auth_headers("https://downloads.example.com/a.bin"),
                {},
            )

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

    def test_gateway_has_all_enabled_model_routes_without_secrets(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml.example", prefer_as_base=True)
        cfg["gateway"]["server_name"] = "114.242.58.129"
        specs = deploy.parse_models(cfg)
        text = deploy.render_gateway_config(cfg, specs)
        self.assertIn("listen 8006 ssl", text)
        self.assertIn("server_name 114.242.58.129", text)
        for key, port in {
            "embedding": 8001,
            "rerank": 8002,
            "verifier": 8003,
            "asr": 8004,
            "tts": 8005,
        }.items():
            self.assertIn(f"location ^~ /{key}/", text)
            self.assertIn(f"proxy_pass http://127.0.0.1:{port}/", text)
        self.assertNotIn("MODEL_API_KEY", text)
        self.assertNotIn("Bearer ", text)

    def test_gateway_omits_disabled_model_route(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml.example", prefer_as_base=True)
        cfg["models"]["verifier"]["enabled"] = False
        text = deploy.render_gateway_config(cfg, deploy.parse_models(cfg))
        self.assertNotIn("location ^~ /verifier/", text)

    def test_gateway_rejects_invalid_boundary_values(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml.example", prefer_as_base=True)
        cfg["gateway"]["listen_port"] = 70000
        with self.assertRaises(RuntimeError):
            deploy.parse_gateway(cfg)
        cfg["gateway"]["listen_port"] = 8006
        cfg["gateway"]["server_name"] = "bad host/name"
        with self.assertRaises(RuntimeError):
            deploy.parse_gateway(cfg)


class TestCli(unittest.TestCase):
    def test_config_before_subcommand(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            # --config 在子命令前：由 main() 预处理注入
            code = deploy.main(
                [
                    "--config",
                    str(ROOT / "config.yaml"),
                    "gen-config",
                    "--output-dir",
                    str(out),
                    "--host",
                    "10.0.0.9",
                ]
            )
            self.assertEqual(code, 0)
            data = json.loads((out / "approved_endpoints.json").read_text(encoding="utf-8"))
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
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
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
            gateway = out / "gateway" / "weknora-model-gateway.conf"
            self.assertTrue(gateway.exists())
            self.assertIn("/verifier/", gateway.read_text(encoding="utf-8"))
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


class TestSkipDownload(unittest.TestCase):
    def test_missing_source_marker_never_skips(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            dest = Path(tmp)
            (dest / "a.bin").write_bytes(b"abc")
            deploy.write_checksums(dest)
            skip, reason = deploy.should_skip_model_download(
                dest, "new-hub|awq||modelscope"
            )
            self.assertFalse(skip)
            self.assertEqual(reason, "missing-source-marker")
            self.assertFalse((dest / ".airgap_source").exists())

    def test_matching_source_skips_when_verified(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            dest = Path(tmp)
            (dest / "a.bin").write_bytes(b"abc")
            deploy.write_checksums(dest)
            expected = "hub|awq||hf"
            (dest / ".airgap_source").write_text(expected + "\n", encoding="utf-8")
            skip, reason = deploy.should_skip_model_download(dest, expected)
            self.assertTrue(skip)
            self.assertTrue(reason.startswith("verified:"))

    def test_source_change_forces_redownload(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            dest = Path(tmp)
            (dest / "a.bin").write_bytes(b"abc")
            deploy.write_checksums(dest)
            (dest / ".airgap_source").write_text("old|fp16||hf\n", encoding="utf-8")
            skip, reason = deploy.should_skip_model_download(dest, "new|awq||hf")
            self.assertFalse(skip)
            self.assertTrue(reason.startswith("source-changed:"))


class TestPickBaseImage(unittest.TestCase):
    def test_prefers_preferred_even_if_fallback_local(self) -> None:
        calls: list[str] = []

        def fake_exists(image: str) -> bool:
            calls.append(image)
            return image == "vllm/vllm-openai:latest"

        def fake_require(_action: str) -> None:
            raise RuntimeError("offline")

        original_exists = deploy.docker_image_exists
        original_require = deploy.require_online
        original_run = deploy.run_cmd
        try:
            deploy.docker_image_exists = fake_exists  # type: ignore[assignment]
            deploy.require_online = fake_require  # type: ignore[assignment]
            deploy.run_cmd = lambda *_a, **_k: (_ for _ in ()).throw(RuntimeError("no"))  # type: ignore[assignment]
            chosen = deploy.pick_local_base_image(
                "pytorch/pytorch:2.2.2-cuda12.1-cudnn8-runtime",
                ["vllm/vllm-openai:latest"],
            )
            self.assertEqual(chosen, "vllm/vllm-openai:latest")
            self.assertEqual(calls[0], "pytorch/pytorch:2.2.2-cuda12.1-cudnn8-runtime")
        finally:
            deploy.docker_image_exists = original_exists  # type: ignore[assignment]
            deploy.require_online = original_require  # type: ignore[assignment]
            deploy.run_cmd = original_run  # type: ignore[assignment]

    def test_returns_preferred_when_local(self) -> None:
        original = deploy.docker_image_exists
        try:
            deploy.docker_image_exists = lambda image: image.startswith("pytorch/")  # type: ignore[assignment]
            chosen = deploy.pick_local_base_image(
                "pytorch/pytorch:2.2.2-cuda12.1-cudnn8-runtime",
                ["vllm/vllm-openai:latest"],
            )
            self.assertEqual(chosen, "pytorch/pytorch:2.2.2-cuda12.1-cudnn8-runtime")
        finally:
            deploy.docker_image_exists = original  # type: ignore[assignment]


class TestSingleGpuDefaults(unittest.TestCase):
    def test_gpu_services_request_one_unpinned_device(self) -> None:
        cfg = deploy.load_config(ROOT / "config.yaml.example", prefer_as_base=True)
        self.assertEqual(cfg.get("docker", {}).get("gpu_count"), 1)
        specs = {s.key: s for s in deploy.parse_models(cfg)}
        for key in ("embedding", "verifier", "tts"):
            self.assertIsNone(specs[key].device_ids)
        text = deploy.render_compose(cfg, Path("/mnt/models"), list(specs.values()))
        self.assertNotIn("device_ids:", text)
        self.assertEqual(text.count("              count: 1"), 3)
        self.assertIn("compressed-tensors", text)


class TestDownloadCleanup(unittest.TestCase):
    def test_auto_ms_failure_clears_dest_before_hf(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            dest = Path(tmp) / "model"
            dest.mkdir()
            (dest / "partial.bin").write_bytes(b"dirty")

            def boom(*_a, **_k):
                raise RuntimeError("ms down")

            def hf_ok(model_id, dest_path, **_k):
                ensure = dest_path
                ensure.mkdir(parents=True, exist_ok=True)
                (ensure / "ok.bin").write_bytes(b"hf")
                return ensure

            original_ms = deploy.download_model_ms
            original_hf = deploy.download_model_hf
            try:
                deploy.download_model_ms = boom  # type: ignore[assignment]
                deploy.download_model_hf = hf_ok  # type: ignore[assignment]
                out = deploy.download_model(
                    "ms/id",
                    dest,
                    source="auto",
                    revision=None,
                    retries=1,
                    delay=0,
                    hf_model_id="hf/id",
                )
                self.assertEqual(out, dest)
                self.assertFalse((dest / "partial.bin").exists())
                self.assertTrue((dest / "ok.bin").exists())
            finally:
                deploy.download_model_ms = original_ms  # type: ignore[assignment]
                deploy.download_model_hf = original_hf  # type: ignore[assignment]


class TestYamlQuote(unittest.TestCase):
    def test_json_arg_roundtrip(self) -> None:
        import yaml

        raw = '{"image": 5}'
        doc = yaml.safe_load(f"cmd:\n  - {deploy.yaml_quote(raw)}\n")
        self.assertEqual(doc["cmd"][0], raw)


if __name__ == "__main__":
    unittest.main(verbosity=2)
