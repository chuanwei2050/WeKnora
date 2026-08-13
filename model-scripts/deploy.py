#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
内网离线 AI 模型自动化部署工具

子命令:
  prepare    — 联网机器上下载模型、镜像、依赖并打包
  deploy     — 内网服务器上解压、校验、加载镜像并启动服务
  verify     — 校验模型文件完整性
  gen-config — 仅根据当前配置生成 compose / 登记文件（不下载）

环境变量:
  HF_TOKEN / HUGGING_FACE_HUB_TOKEN  — Hugging Face 访问令牌（不进包）
  MODELSCOPE_TOKEN                   — ModelScope 令牌（可选）
  AIR_GAPPED_MODE=true               — 跳过一切互联网请求
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import socket
import subprocess
import sys
import tarfile
import tempfile
import time
import zipfile
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple

SCRIPT_DIR = Path(__file__).resolve().parent

# 量化关键字：若配置了这些 quant 且未设 download_id，prepare 会失败（避免下错全精度权重）
QUANT_NEEDS_DOWNLOAD_ID = {
    "awq",
    "awq-int4",
    "gptq",
    "fp8",
    "onnx-int8",
    "gguf",
    "gguf-q4",
}


# ---------------------------------------------------------------------------
# 工具函数
# ---------------------------------------------------------------------------


def eprint(*args: Any, **kwargs: Any) -> None:
    print(*args, file=sys.stderr, **kwargs)


def is_air_gapped() -> bool:
    return os.environ.get("AIR_GAPPED_MODE", "").strip().lower() in {
        "1",
        "true",
        "yes",
        "on",
    }


def require_online(action: str) -> None:
    if is_air_gapped():
        raise RuntimeError(f"AIR_GAPPED_MODE=true，禁止执行需要联网的操作: {action}")


def yaml_quote(value: str) -> str:
    """安全生成 YAML 双引号标量（处理内含引号 / 花括号的 vLLM 参数）。"""
    escaped = (
        value.replace("\\", "\\\\")
        .replace('"', '\\"')
        .replace("\n", "\\n")
    )
    return f'"{escaped}"'


def load_yaml(path: Path) -> Dict[str, Any]:
    try:
        import yaml
    except ImportError as exc:
        raise RuntimeError("缺少 PyYAML，请先: pip install -r requirements.txt") from exc
    with path.open("r", encoding="utf-8") as fh:
        data = yaml.safe_load(fh) or {}
    if not isinstance(data, dict):
        raise RuntimeError(f"配置文件格式错误: {path}")
    return data


def dump_yaml(path: Path, data: Dict[str, Any]) -> None:
    import yaml

    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as fh:
        yaml.safe_dump(data, fh, allow_unicode=True, sort_keys=False)


def deep_merge(base: Dict[str, Any], override: Dict[str, Any]) -> Dict[str, Any]:
    out = dict(base)
    for key, value in override.items():
        if key in out and isinstance(out[key], dict) and isinstance(value, dict):
            out[key] = deep_merge(out[key], value)
        else:
            out[key] = value
    return out


def run_cmd(
    cmd: List[str],
    *,
    cwd: Optional[Path] = None,
    check: bool = True,
    env: Optional[Dict[str, str]] = None,
    capture: bool = False,
) -> subprocess.CompletedProcess:
    print(f"+ {' '.join(cmd)}")
    merged_env = os.environ.copy()
    if env:
        merged_env.update(env)
    result = subprocess.run(
        cmd,
        cwd=str(cwd) if cwd else None,
        env=merged_env,
        check=False,
        text=True,
        capture_output=capture,
    )
    if check and result.returncode != 0:
        stderr = (result.stderr or "").strip()
        raise RuntimeError(f"命令失败 ({result.returncode}): {' '.join(cmd)}\n{stderr}")
    return result


def file_checksum(path: Path, algo: str = "sha256", chunk: int = 1024 * 1024) -> str:
    h = hashlib.new(algo)
    with path.open("rb") as fh:
        while True:
            block = fh.read(chunk)
            if not block:
                break
            h.update(block)
    return h.hexdigest()


def write_checksums(root: Path, algo: str = "sha256") -> Path:
    out = root / f".checksums.{algo}"
    lines: List[str] = []
    for path in sorted(p for p in root.rglob("*") if p.is_file()):
        if path.name.startswith(".checksums"):
            continue
        rel = path.relative_to(root).as_posix()
        lines.append(f"{file_checksum(path, algo)}  {rel}")
    if not lines:
        raise RuntimeError(f"目录为空，无法生成校验清单: {root}")
    out.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return out


def verify_checksums(root: Path, algo: str = "sha256") -> Tuple[int, List[str]]:
    checksum_file = root / f".checksums.{algo}"
    if not checksum_file.exists():
        alt = root / ".checksums"
        if alt.exists():
            checksum_file = alt
        else:
            raise FileNotFoundError(f"未找到校验文件: {checksum_file}")

    errors: List[str] = []
    ok = 0
    entries = 0
    for raw in checksum_file.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        entries += 1
        parts = line.split(None, 1)
        if len(parts) != 2:
            errors.append(f"格式错误: {line}")
            continue
        expected, rel = parts[0], parts[1].lstrip("*").strip()
        target = root / rel
        if not target.is_file():
            errors.append(f"缺失: {rel}")
            continue
        if len(expected) == 32:
            actual = file_checksum(target, "md5")
        elif len(expected) == 64:
            actual = file_checksum(target, "sha256")
        else:
            actual = file_checksum(target, algo)
        if actual.lower() != expected.lower():
            errors.append(f"不匹配: {rel}")
        else:
            ok += 1
    if entries == 0:
        errors.append("校验清单为空（0 条记录）")
    if ok == 0 and not any(e.startswith("校验清单为空") for e in errors):
        errors.append("没有任何文件通过校验")
    return ok, errors


def port_in_use(port: int, host: str = "127.0.0.1") -> bool:
    """通过 connect 探测端口是否已有监听者（比 bind+SO_REUSEADDR 更可靠）。"""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.settimeout(0.5)
        try:
            sock.connect((host, port))
            return True
        except OSError:
            return False


def ensure_dir(path: Path) -> Path:
    path.mkdir(parents=True, exist_ok=True)
    return path


def progress_iter(iterable: Iterable, desc: str = ""):
    try:
        from tqdm import tqdm

        return tqdm(iterable, desc=desc)
    except ImportError:
        return iterable


def safe_extract_tar(archive: Path, dest: Path) -> None:
    dest = dest.resolve()
    with tarfile.open(archive, "r:*") as tar:
        members = tar.getmembers()
        for member in members:
            member_path = (dest / member.name).resolve()
            if not str(member_path).startswith(str(dest) + os.sep) and member_path != dest:
                raise RuntimeError(f"不安全的归档路径（疑似路径穿越）: {member.name}")
        if sys.version_info >= (3, 12):
            tar.extractall(dest, filter="data")  # type: ignore[call-arg]
        else:
            tar.extractall(dest)


def safe_extract_zip(archive: Path, dest: Path) -> None:
    dest = dest.resolve()
    with zipfile.ZipFile(archive, "r") as zf:
        for name in zf.namelist():
            member_path = (dest / name).resolve()
            if not str(member_path).startswith(str(dest) + os.sep) and member_path != dest:
                raise RuntimeError(f"不安全的归档路径（疑似路径穿越）: {name}")
        zf.extractall(dest)


def offline_env_pairs() -> List[str]:
    return [
        "AIR_GAPPED_MODE=true",
        "HF_HUB_OFFLINE=1",
        "TRANSFORMERS_OFFLINE=1",
        "HF_DATASETS_OFFLINE=1",
        "VLLM_NO_USAGE_STATS=1",
    ]


# ---------------------------------------------------------------------------
# 配置模型
# ---------------------------------------------------------------------------


@dataclass
class ModelSpec:
    key: str
    enabled: bool
    role: str
    model_id: str
    quant: str
    engine: str
    port: int
    served_model_name: str
    revision: Optional[str] = None
    download_id: Optional[str] = None
    max_model_len: int = 8192
    gpu_memory_utilization: float = 0.9
    dtype: str = "auto"
    limit_mm_per_prompt: Optional[str] = None
    device_ids: Optional[List[int]] = None
    extra_vllm_args: List[str] = field(default_factory=list)
    allowed_uses: List[str] = field(default_factory=list)
    forbidden_uses: List[str] = field(default_factory=list)
    # 额外角色（与主 role 共用同一端点/权重），例如主模型 roles 含 vlm
    roles: List[str] = field(default_factory=list)

    @property
    def hub_id(self) -> str:
        return (self.download_id or self.model_id).strip()

    @property
    def local_dirname(self) -> str:
        return self.key

    @property
    def all_roles(self) -> List[str]:
        out: List[str] = []
        for r in [self.role, *self.roles]:
            r = str(r or "").strip()
            if r and r not in out:
                out.append(r)
        return out or [self.key]


def parse_models(cfg: Dict[str, Any]) -> List[ModelSpec]:
    models_cfg = cfg.get("models") or {}
    specs: List[ModelSpec] = []
    for key, raw in models_cfg.items():
        if not isinstance(raw, dict):
            continue
        port = int(raw.get("port") or 0)
        enabled = bool(raw.get("enabled", True))
        if enabled and not (1 <= port <= 65535):
            raise RuntimeError(f"模型 {key} 的 port 无效: {port}（启用模型必须为 1-65535）")
        device_ids = raw.get("device_ids")
        if device_ids is not None:
            device_ids = [int(x) for x in device_ids]
        download_id = raw.get("download_id")
        if download_id is not None:
            download_id = str(download_id).strip() or None
        limit_mm = raw.get("limit_mm_per_prompt")
        if isinstance(limit_mm, (dict, list)):
            limit_mm = json.dumps(limit_mm, ensure_ascii=False, separators=(",", ":"))
        elif limit_mm is not None:
            limit_mm = str(limit_mm)
        extra_roles = list(raw.get("roles") or [])
        # 兼容 roles: [chat, vlm] 写法：若含主 role 则去重保留其余
        specs.append(
            ModelSpec(
                key=key,
                enabled=enabled,
                role=str(raw.get("role") or key),
                model_id=str(raw.get("model_id") or ""),
                quant=str(raw.get("quant") or ""),
                engine=str(raw.get("engine") or "vllm"),
                port=port,
                served_model_name=str(raw.get("served_model_name") or key),
                revision=raw.get("revision"),
                download_id=download_id,
                max_model_len=int(raw.get("max_model_len") or 8192),
                gpu_memory_utilization=float(raw.get("gpu_memory_utilization") or 0.9),
                dtype=str(raw.get("dtype") or "auto"),
                limit_mm_per_prompt=limit_mm,
                device_ids=device_ids,
                extra_vllm_args=list(raw.get("extra_vllm_args") or []),
                allowed_uses=list(raw.get("allowed_uses") or [raw.get("role") or key]),
                forbidden_uses=list(raw.get("forbidden_uses") or []),
                roles=[str(r) for r in extra_roles if str(r).strip()],
            )
        )
    return specs


def validate_specs_for_prepare(specs: List[ModelSpec]) -> None:
    for spec in [s for s in specs if s.enabled]:
        quant = spec.quant.lower().strip()
        needs = any(q in quant for q in QUANT_NEEDS_DOWNLOAD_ID) or quant in QUANT_NEEDS_DOWNLOAD_ID
        # onnx/fp8/awq：允许 download_id；若与 model_id 相同也可（社区仓即量化仓）
        if needs and not spec.download_id:
            raise RuntimeError(
                f"模型 {spec.key} quant={spec.quant} 需要设置 download_id 指向社区量化仓库，"
                f"或将 quant 改为与 model_id 实际权重一致（如 bf16/fp16）。"
                f"当前 hub 将下载: {spec.model_id}"
            )
        if not spec.model_id:
            raise RuntimeError(f"模型 {spec.key} 缺少 model_id")


def resolve_config_path(cli_config: Optional[str]) -> Optional[Path]:
    if cli_config:
        return Path(cli_config)
    # 默认优先使用同目录 config.yaml（README 的 cp example 流程）
    local = SCRIPT_DIR / "config.yaml"
    if local.exists():
        return local
    return None


def load_config(path: Optional[Path], *, prefer_as_base: bool = False) -> Dict[str, Any]:
    """
    默认：example 为底，用户文件 deep_merge 覆盖。
    prefer_as_base=True 或用户设置 models_replace: true 时，以用户文件 models 为主体。
    """
    example = SCRIPT_DIR / "config.yaml.example"
    if not example.exists():
        raise RuntimeError(f"缺少默认配置: {example}")
    example_cfg = load_yaml(example)

    if path is None:
        path = resolve_config_path(None)
    if path is None:
        return example_cfg
    if not path.exists():
        eprint(f"警告: 配置文件不存在，使用示例默认值: {path}")
        return example_cfg

    user_cfg = load_yaml(path)
    replace_models = bool(user_cfg.pop("models_replace", False)) or prefer_as_base
    if replace_models:
        cfg = deep_merge(example_cfg, user_cfg)
        if "models" in user_cfg:
            cfg["models"] = user_cfg["models"]
        return cfg
    return deep_merge(example_cfg, user_cfg)


# ---------------------------------------------------------------------------
# 下载
# ---------------------------------------------------------------------------


def download_model_hf(
    model_id: str,
    dest: Path,
    *,
    revision: Optional[str] = None,
    retries: int = 3,
    delay: float = 5.0,
) -> Path:
    require_online(f"huggingface download {model_id}")
    try:
        from huggingface_hub import snapshot_download
    except ImportError as exc:
        raise RuntimeError("缺少 huggingface_hub，请先安装 requirements.txt") from exc

    token = os.environ.get("HF_TOKEN") or os.environ.get("HUGGING_FACE_HUB_TOKEN")
    last_err: Optional[Exception] = None
    for attempt in range(1, retries + 1):
        try:
            print(f"[HF] 下载 {model_id} -> {dest} (尝试 {attempt}/{retries})")
            ensure_dir(dest)
            snapshot_download(
                repo_id=model_id,
                local_dir=str(dest),
                revision=revision,
                token=token,
            )
            return dest
        except Exception as exc:  # noqa: BLE001
            last_err = exc
            eprint(f"[HF] 失败: {exc}")
            if attempt < retries:
                time.sleep(delay)
    raise RuntimeError(f"Hugging Face 下载失败: {model_id}: {last_err}")


def download_model_ms(
    model_id: str,
    dest: Path,
    *,
    revision: Optional[str] = None,
    retries: int = 3,
    delay: float = 5.0,
) -> Path:
    require_online(f"modelscope download {model_id}")
    try:
        from modelscope.hub.snapshot_download import snapshot_download as ms_snapshot
    except ImportError as exc:
        raise RuntimeError("缺少 modelscope，请 pip install modelscope 或改用 huggingface") from exc

    token = os.environ.get("MODELSCOPE_TOKEN")
    last_err: Optional[Exception] = None
    for attempt in range(1, retries + 1):
        try:
            print(f"[ModelScope] 下载 {model_id} -> {dest} (尝试 {attempt}/{retries})")
            ensure_dir(dest.parent)
            kwargs: Dict[str, Any] = {"model_id": model_id}
            # 兼容不同 modelscope 签名
            try:
                cache_path = ms_snapshot(model_id, revision=revision, token=token)
            except TypeError:
                if token:
                    os.environ.setdefault("MODELSCOPE_API_TOKEN", token)
                cache_path = ms_snapshot(model_id, revision=revision)
            if Path(cache_path).resolve() != dest.resolve():
                if dest.exists():
                    shutil.rmtree(dest)
                shutil.copytree(cache_path, dest)
            return dest
        except Exception as exc:  # noqa: BLE001
            last_err = exc
            eprint(f"[ModelScope] 失败: {exc}")
            if attempt < retries:
                time.sleep(delay)
    raise RuntimeError(f"ModelScope 下载失败: {model_id}: {last_err}")


def download_model(
    model_id: str,
    dest: Path,
    *,
    source: str,
    revision: Optional[str],
    retries: int,
    delay: float,
) -> Path:
    source = (source or "auto").lower()
    if source == "huggingface":
        return download_model_hf(model_id, dest, revision=revision, retries=retries, delay=delay)
    if source == "modelscope":
        return download_model_ms(model_id, dest, revision=revision, retries=retries, delay=delay)
    try:
        return download_model_ms(model_id, dest, revision=revision, retries=retries, delay=delay)
    except Exception as ms_err:  # noqa: BLE001
        eprint(f"[auto] ModelScope 不可用，回退 Hugging Face: {ms_err}")
        return download_model_hf(model_id, dest, revision=revision, retries=retries, delay=delay)


def pip_download_offline(req_files: List[Path], dest: Path) -> None:
    require_online("pip download")
    ensure_dir(dest)
    for req in req_files:
        if not req.exists():
            eprint(f"警告: 依赖清单不存在，跳过: {req}")
            continue
        run_cmd(
            [
                sys.executable,
                "-m",
                "pip",
                "download",
                "-r",
                str(req),
                "-d",
                str(dest),
            ]
        )


def docker_pull_and_save(image: str, tar_path: Path) -> None:
    require_online(f"docker pull {image}")
    ensure_dir(tar_path.parent)
    run_cmd(["docker", "pull", image])
    run_cmd(["docker", "save", "-o", str(tar_path), image])
    print(f"已导出镜像: {tar_path} ({tar_path.stat().st_size / (1024**3):.2f} GiB)")


def docker_build_and_save(
    *,
    tag: str,
    dockerfile: Path,
    context: Path,
    tar_path: Path,
) -> None:
    require_online(f"docker build {tag}")
    ensure_dir(tar_path.parent)
    if not dockerfile.exists():
        raise FileNotFoundError(f"缺少 Dockerfile: {dockerfile}")
    run_cmd(
        [
            "docker",
            "build",
            "-t",
            tag,
            "-f",
            str(dockerfile),
            str(context),
        ]
    )
    run_cmd(["docker", "save", "-o", str(tar_path), tag])
    print(f"已导出镜像: {tar_path} ({tar_path.stat().st_size / (1024**3):.2f} GiB)")


def service_image_tag(cfg: Dict[str, Any], key: str) -> str:
    docker = cfg.get("docker") or {}
    images = docker.get("service_images") or {}
    return str(images.get(key) or f"weknora-{key}:airgap")


def service_image_tar(cfg: Dict[str, Any], key: str) -> str:
    docker = cfg.get("docker") or {}
    tars = docker.get("service_image_tars") or {}
    return str(tars.get(key) or f"images/weknora-{key}.tar")


def is_vllm_engine(engine: str) -> bool:
    return engine == "vllm"


def is_container_engine(engine: str) -> bool:
    # python/docker 作为历史别名，统一走容器
    return engine in {"container", "python", "docker"}


def vendor_cosyvoice(target_dir: Path) -> Path:
    """将 CosyVoice 源码拉到 target_dir/CosyVoice（用于 TTS 镜像构建）。"""
    require_online("git clone CosyVoice")
    ensure_dir(target_dir)
    target = target_dir / "CosyVoice"
    if target.exists():
        shutil.rmtree(target)
    url = os.environ.get(
        "COSYVOICE_GIT_URL",
        "https://github.com/FunAudioLLM/CosyVoice.git",
    )
    print(f"\n=== 拉取 CosyVoice 运行时源码: {url} ===")
    run_cmd(["git", "clone", "--depth", "1", url, str(target)])
    run_cmd(
        ["git", "submodule", "update", "--init", "--recursive", "--depth", "1"],
        cwd=target,
        check=True,
    )
    return target


def prepare_container_images(
    cfg: Dict[str, Any],
    specs: List[ModelSpec],
    output_dir: Path,
    *,
    skip_build: bool = False,
) -> None:
    """构建/导出 Rerank、ASR、TTS 镜像，并导出 vLLM 镜像。"""
    docker_cfg = cfg.get("docker") or {}
    enabled = [s for s in specs if s.enabled]

    # vLLM
    vllm_needed = any(is_vllm_engine(s.engine) for s in enabled)
    if vllm_needed:
        image = docker_cfg.get("vllm_image") or "vllm/vllm-openai:latest"
        tar_path = output_dir / (docker_cfg.get("vllm_image_tar") or "images/vllm-openai.tar")
        print(f"\n=== 拉取并导出 vLLM 镜像 {image} ===")
        print(f"提示: Chat/VLM FP8 需要 vLLM >= {docker_cfg.get('vllm_min_version_hint', '0.19.0')}")
        docker_pull_and_save(image, tar_path)

    if skip_build:
        print("跳过业务镜像构建 (--skip-docker-build)")
        return

    # CosyVoice → TTS 构建上下文
    tts_specs = [s for s in enabled if s.key == "tts" and is_container_engine(s.engine)]
    tts_ctx = SCRIPT_DIR / "services" / "tts"
    if tts_specs:
        vendor_dir = tts_ctx / "vendor"
        try:
            vendor_cosyvoice(vendor_dir)
            # 同步一份到包内，便于审计
            bundle_vendor = ensure_dir(output_dir / "vendor")
            dst = bundle_vendor / "CosyVoice"
            if dst.exists():
                shutil.rmtree(dst)
            shutil.copytree(vendor_dir / "CosyVoice", dst)
        except Exception as exc:  # noqa: BLE001
            raise RuntimeError(f"TTS 镜像构建需要 CosyVoice 源码: {exc}") from exc

    for key in ("rerank", "asr", "tts"):
        if not any(s.key == key and is_container_engine(s.engine) for s in enabled):
            continue
        svc_dir = SCRIPT_DIR / "services" / key
        dockerfile = svc_dir / "Dockerfile"
        tag = service_image_tag(cfg, key)
        tar_path = output_dir / service_image_tar(cfg, key)
        print(f"\n=== 构建并导出 {key} 镜像 {tag} ===")
        docker_build_and_save(
            tag=tag,
            dockerfile=dockerfile,
            context=svc_dir,
            tar_path=tar_path,
        )


# ---------------------------------------------------------------------------
# 生成制品
# ---------------------------------------------------------------------------


def model_host_path(data_dir: Path, spec: ModelSpec) -> Path:
    return data_dir / "models" / spec.local_dirname


def _gpu_device_block(spec: ModelSpec, default_count: Any) -> List[str]:
    if spec.device_ids:
        quoted = ", ".join(f'"{i}"' for i in spec.device_ids)
        return [
            "            - driver: nvidia",
            f"              device_ids: [{quoted}]",
            "              capabilities: [gpu]",
        ]
    count = default_count if default_count not in (None, "", "all") else 1
    return [
        "            - driver: nvidia",
        f"              count: {count}",
        "              capabilities: [gpu]",
    ]


def _compose_health_vllm() -> str:
    return (
        '      test: ["CMD", "python", "-c", '
        '"import urllib.request; urllib.request.urlopen(\'http://127.0.0.1:8000/v1/models\')"]'
    )


def _compose_health_app() -> str:
    return (
        '      test: ["CMD", "python", "-c", '
        '"import urllib.request,json; '
        "d=json.load(urllib.request.urlopen('http://127.0.0.1:8000/healthz')); "
        'assert d.get(\'ready\') is True"]'
    )


def render_compose(cfg: Dict[str, Any], data_dir: Path, specs: List[ModelSpec]) -> str:
    docker = cfg.get("docker") or {}
    vllm_image = docker.get("vllm_image") or "vllm/vllm-openai:latest"
    shm = docker.get("shm_size") or "16gb"
    default_gpu = docker.get("gpu_count", 1)
    lines = [
        "# 由 model-scripts/deploy.py 自动生成 — 全容器离线模型编排",
        "# 一键启动: docker compose -f docker-compose.airgap.override.yml up -d",
        "",
        "services:",
    ]
    service_count = 0

    for spec in specs:
        if not spec.enabled:
            continue
        host_model = model_host_path(data_dir, spec).as_posix()
        env_lines = "\n".join(f"      - {p}" for p in offline_env_pairs())

        if is_vllm_engine(spec.engine):
            service_count += 1
            args = [
                "--model",
                "/models",
                "--served-model-name",
                spec.served_model_name,
                "--max-model-len",
                str(spec.max_model_len),
                "--gpu-memory-utilization",
                str(spec.gpu_memory_utilization),
                "--dtype",
                spec.dtype,
                "--host",
                "0.0.0.0",
                "--port",
                "8000",
            ]
            if spec.limit_mm_per_prompt:
                args.extend(["--limit-mm-per-prompt", str(spec.limit_mm_per_prompt)])
            # Embedding 模型需要 pooling runner，否则 /v1/embeddings 不可用
            if "embedding" in spec.all_roles and "--runner" not in args:
                args.extend(["--runner", "pooling"])
            args.extend(spec.extra_vllm_args)
            cmd_yaml = "\n".join(f"      - {yaml_quote(a)}" for a in args)
            gpu_lines = _gpu_device_block(spec, default_gpu)
            lines.extend(
                [
                    f"  {spec.key}:",
                    f"    image: {vllm_image}",
                    "    pull_policy: never",
                    "    restart: unless-stopped",
                    f"    shm_size: {shm}",
                    "    ipc: host",
                    "    deploy:",
                    "      resources:",
                    "        reservations:",
                    "          devices:",
                    *gpu_lines,
                    "    ports:",
                    f'      - "{spec.port}:8000"',
                    "    volumes:",
                    f'      - "{host_model}:/models:ro"',
                    "    environment:",
                    env_lines,
                    "    healthcheck:",
                    _compose_health_vllm(),
                    "      interval: 30s",
                    "      timeout: 10s",
                    "      retries: 10",
                    "      start_period: 120s",
                    "    command:",
                    cmd_yaml,
                    "",
                ]
            )
            continue

        if is_container_engine(spec.engine):
            service_count += 1
            image = service_image_tag(cfg, spec.key)
            needs_gpu = bool(
                (cfg.get("models") or {}).get(spec.key, {}).get(
                    "needs_gpu",
                    spec.key in {"asr", "tts"},
                )
            )
            block = [
                f"  {spec.key}:",
                f"    image: {image}",
                "    pull_policy: never",
                "    restart: unless-stopped",
            ]
            if needs_gpu:
                gpu_lines = _gpu_device_block(spec, default_gpu)
                block.extend(
                    [
                        "    deploy:",
                        "      resources:",
                        "        reservations:",
                        "          devices:",
                        *gpu_lines,
                    ]
                )
            app_env = [
                env_lines,
                "      - MODEL_DIR=/models",
                "      - PORT=8000",
                f"      - SERVED_MODEL_NAME={spec.served_model_name}",
                f"      - QUANT={spec.quant}",
            ]
            block.extend(
                [
                    "    ports:",
                    f'      - "{spec.port}:8000"',
                    "    volumes:",
                    f'      - "{host_model}:/models:ro"',
                    "    environment:",
                    *app_env,
                    "    healthcheck:",
                    _compose_health_app(),
                    "      interval: 30s",
                    "      timeout: 10s",
                    "      retries: 15",
                    "      start_period: 180s",
                    "",
                ]
            )
            lines.extend(block)
            continue

        eprint(f"警告: 未知 engine={spec.engine}，跳过 compose 服务 {spec.key}")

    if service_count == 0:
        lines.append("  # （当前无启用服务）")
        lines.append("  placeholder:")
        lines.append("    image: alpine:3.20")
        lines.append("    pull_policy: never")
        lines.append('    command: ["true"]')
        lines.append('    profiles: ["disabled"]')
    return "\n".join(lines) + "\n"


def render_approved_endpoints(cfg: Dict[str, Any], specs: List[ModelSpec]) -> Dict[str, Any]:
    deploy = cfg.get("deploy") or {}
    host = str(deploy.get("host") or "127.0.0.1")
    scheme = str(deploy.get("scheme") or "http")
    endpoints = []
    registry = []
    for spec in specs:
        if not spec.enabled:
            continue
        roles = spec.all_roles
        allowed = [u for u in spec.allowed_uses if u not in set(spec.forbidden_uses)]
        for role in roles:
            if role not in allowed:
                allowed.append(role)
        if "model" not in allowed:
            allowed.append("model")
        ep = {
            "id": f"model-{spec.key}",
            "scheme": scheme,
            "host": host,
            "port": spec.port,
            "protocol": "openai-compatible",
            "tls_required": scheme.lower() == "https",
            "category": "model",
            "allowed_uses": allowed,
            "allowed_model_roles": roles,
            "endpoint_url": f"{scheme}://{host}:{spec.port}/v1",
            "notes": (
                f"禁止用途: {', '.join(spec.forbidden_uses) or '无'}; quant={spec.quant}; "
                f"roles={','.join(roles)}. "
                "WeKnora Create API 会分配新 UUID，导入后请把 model_registry 中的 "
                "approved_endpoint_id 改成平台返回的真实 ID。"
            ),
        }
        endpoints.append(ep)
        for role in roles:
            registry.append(
                {
                    "role": role,
                    "model_name": spec.served_model_name,
                    "display_name": f"{role}:{spec.served_model_name}",
                    "endpoint_url": ep["endpoint_url"],
                    "approved_endpoint_id": ep["id"],
                    "engine": spec.engine,
                    "quant": spec.quant,
                    "source_model_id": spec.model_id,
                    "shared_service": spec.key,
                }
            )
    return {
        "approved_endpoints": endpoints,
        "model_registry": registry,
        "meta": {
            "generated_by": "model-scripts/deploy.py",
            "air_gapped_mode": True,
            "host": host,
        },
    }


def write_bundle_readme(bundle_dir: Path, cfg: Dict[str, Any]) -> None:
    deploy = cfg.get("deploy") or {}
    host = deploy.get("host") or "<内网IP>"
    data_dir = deploy.get("data_dir") or "/mnt/models"
    text = f"""# 离线模型部署包（全容器）

## 传输到内网

1. 将 `*.tar.gz` 拷贝到内网服务器（建议 `/tmp/offline-model-bundle.tar.gz`）。
2. **不要**把 `HF_TOKEN` / `MODELSCOPE_TOKEN` 写入介质。

## 一键部署

```bash
export AIR_GAPPED_MODE=true
python deploy.py deploy \\
  --bundle /tmp/offline-model-bundle.tar.gz \\
  --data-dir {data_dir} \\
  --host {host}
```

等价于：解压 → 校验权重 → `docker load` 全部镜像 → `docker compose up -d`。

包内 `config.yaml` 会自动优先使用。

## 仅启动 / 停止

```bash
docker compose -p weknora-models -f {data_dir}/docker-compose.airgap.override.yml up -d
docker compose -p weknora-models -f {data_dir}/docker-compose.airgap.override.yml down
```

## 平台登记

- `{data_dir}/registry/approved_endpoints.json`
- `{data_dir}/registry/model_registry.yaml`

VLM 与 Chat 共用主模型端点（启用 `models.chat` 后生效）。
"""
    (bundle_dir / "README.md").write_text(text, encoding="utf-8")


def render_systemd_unit(spec: ModelSpec, data_dir: Path) -> str:
    return f"""[Unit]
Description=WeKnora Airgap {spec.role} ({spec.served_model_name})
After=network.target
Wants=network-online.target

[Service]
Type=simple
Environment=AIR_GAPPED_MODE=true
Environment=HF_HUB_OFFLINE=1
Environment=TRANSFORMERS_OFFLINE=1
Environment=MODEL_DIR={data_dir.as_posix()}/models/{spec.key}
Environment=SERVED_MODEL_NAME={spec.served_model_name}
Environment=PORT={spec.port}
Environment=QUANT={spec.quant}
Environment=PYTHONPATH={data_dir.as_posix()}/vendor/CosyVoice:{data_dir.as_posix()}/vendor/CosyVoice/third_party/Matcha-TTS
WorkingDirectory={data_dir.as_posix()}/services/{spec.key}
ExecStart={sys.executable} server.py
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
"""


# ---------------------------------------------------------------------------
# 子命令
# ---------------------------------------------------------------------------


def cmd_prepare(args: argparse.Namespace) -> int:
    require_online("prepare")
    cfg = load_config(resolve_config_path(args.config))
    if args.output_dir:
        cfg.setdefault("bundle", {})["output_dir"] = args.output_dir

    bundle_cfg = cfg.get("bundle") or {}
    output_dir = Path(bundle_cfg.get("output_dir") or "./offline-bundle").resolve()
    algo = str(bundle_cfg.get("checksum_algo") or "sha256")
    source = str(bundle_cfg.get("download_source") or "auto")
    retries = int(bundle_cfg.get("download_retries") or 3)
    delay = float(bundle_cfg.get("download_retry_delay_sec") or 5)

    if output_dir.exists() and args.clean:
        shutil.rmtree(output_dir)
    ensure_dir(output_dir)

    for name in (
        "deploy.py",
        "config.yaml.example",
        "requirements.txt",
        "docker-compose.airgap.override.yml",
    ):
        src = SCRIPT_DIR / name
        if src.exists():
            shutil.copy2(src, output_dir / name)
    for sub in ("services", "systemd", "templates"):
        src = SCRIPT_DIR / sub
        if src.exists():
            dst = output_dir / sub
            if dst.exists():
                shutil.rmtree(dst)
            shutil.copytree(
                src,
                dst,
                ignore=shutil.ignore_patterns("vendor", "__pycache__", "*.pyc"),
            )

    dump_yaml(output_dir / "config.yaml", cfg)

    models_root = ensure_dir(output_dir / "models")
    specs = parse_models(cfg)
    enabled = [s for s in specs if s.enabled]
    if not enabled:
        raise RuntimeError("没有启用的模型，请检查 config.yaml")
    validate_specs_for_prepare(enabled)

    for spec in enabled:
        dest = models_root / spec.local_dirname
        print(f"\n=== 下载模型 [{spec.role}] {spec.hub_id} (quant={spec.quant}) ===")
        download_model(
            spec.hub_id,
            dest,
            source=source,
            revision=spec.revision,
            retries=retries,
            delay=delay,
        )
        checksum_path = write_checksums(dest, algo=algo)
        shutil.copy2(checksum_path, dest / ".checksums")
        print(f"校验清单: {checksum_path}")

    if args.skip_docker:
        print("跳过所有 Docker 镜像拉取/构建 (--skip-docker)")
    else:
        prepare_container_images(
            cfg,
            specs,
            output_dir,
            skip_build=bool(args.skip_docker_build),
        )

    # 可选：仍下载工具自身 pip 依赖，方便内网装 deploy.py 依赖（非服务运行时）
    if args.skip_pip:
        print("跳过 pip download (--skip-pip)")
    else:
        pkg_dir = ensure_dir(output_dir / "offline_packages")
        print("\n=== 下载 deploy 工具离线依赖（非模型服务运行时）===")
        pip_download_offline([SCRIPT_DIR / "requirements.txt"], pkg_dir)

    data_dir = Path((cfg.get("deploy") or {}).get("data_dir") or "/mnt/models")
    compose_text = render_compose(cfg, data_dir, specs)
    import yaml

    yaml.safe_load(compose_text)
    (output_dir / "docker-compose.airgap.override.yml").write_text(compose_text, encoding="utf-8")
    registry = render_approved_endpoints(cfg, specs)
    reg_dir = ensure_dir(output_dir / "registry")
    (reg_dir / "approved_endpoints.json").write_text(
        json.dumps(registry, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    dump_yaml(reg_dir / "model_registry.yaml", {"models": registry["model_registry"]})
    write_bundle_readme(output_dir, cfg)
    print("\n=== 生成顶层校验清单 ===")
    write_checksums(models_root, algo=algo)

    archive_path = output_dir.with_suffix("").parent / f"{output_dir.name}.tar.gz"
    if args.no_archive:
        print(f"跳过打包。材料目录: {output_dir}")
    else:
        print(f"\n=== 打包 {archive_path} ===")
        if archive_path.exists():
            archive_path.unlink()
        with tarfile.open(archive_path, "w:gz") as tar:
            tar.add(output_dir, arcname=output_dir.name)
        print(f"完成: {archive_path} ({archive_path.stat().st_size / (1024**3):.2f} GiB)")

    print("\nprepare 成功。请将归档拷贝到内网后执行 deploy。")
    return 0


def _extract_bundle(bundle: Path, dest: Path) -> Path:
    ensure_dir(dest)
    if bundle.is_dir():
        return bundle.resolve()
    if not bundle.exists():
        raise FileNotFoundError(f"部署包不存在: {bundle}")

    print(f"解压 {bundle} -> {dest}")
    name = bundle.name.lower()
    if name.endswith(".tar.gz") or name.endswith(".tgz") or name.endswith(".tar"):
        safe_extract_tar(bundle, dest)
    elif name.endswith(".zip"):
        safe_extract_zip(bundle, dest)
    else:
        raise RuntimeError(f"不支持的包格式: {bundle}")

    children = [p for p in dest.iterdir() if not p.name.startswith(".")]
    if len(children) == 1 and children[0].is_dir():
        return children[0]
    return dest


def _install_python_deps(bundle_dir: Path, cfg: Dict[str, Any]) -> None:
    py_cfg = cfg.get("python_services") or {}
    pkg_dir = bundle_dir / (py_cfg.get("offline_packages_dir") or "offline_packages")
    if not pkg_dir.exists():
        eprint(f"警告: 离线包目录不存在: {pkg_dir}")
        return
    req_files = []
    for rel in py_cfg.get("requirements") or []:
        cand = bundle_dir / rel
        if not cand.exists():
            cand = SCRIPT_DIR / rel
        if cand.exists():
            req_files.append(cand)
    root_req = bundle_dir / "requirements.txt"
    if root_req.exists():
        req_files.append(root_req)
    cosy_req = bundle_dir / "vendor" / "CosyVoice" / "requirements.txt"
    if cosy_req.exists():
        req_files.append(cosy_req)
    for req in req_files:
        run_cmd(
            [
                sys.executable,
                "-m",
                "pip",
                "install",
                "--no-index",
                f"--find-links={pkg_dir}",
                "-r",
                str(req),
            ]
        )


def _pythonpath_for(data_dir: Path) -> str:
    parts = [
        (data_dir / "vendor" / "CosyVoice").as_posix(),
        (data_dir / "vendor" / "CosyVoice" / "third_party" / "Matcha-TTS").as_posix(),
    ]
    return os.pathsep.join(parts)


def _write_start_scripts(data_dir: Path, services_root: Path, specs: List[ModelSpec]) -> None:
    bin_dir = ensure_dir(data_dir / "bin")
    py_path = _pythonpath_for(data_dir)
    for spec in specs:
        if not spec.enabled or spec.engine != "python":
            continue
        model_dir = model_host_path(data_dir, spec)
        service_dir = services_root / "services" / spec.key
        if not service_dir.exists():
            service_dir = SCRIPT_DIR / "services" / spec.key

        env_exports = {
            "AIR_GAPPED_MODE": "true",
            "HF_HUB_OFFLINE": "1",
            "TRANSFORMERS_OFFLINE": "1",
            "MODEL_DIR": model_dir.as_posix(),
            "SERVED_MODEL_NAME": spec.served_model_name,
            "PORT": str(spec.port),
            "QUANT": spec.quant,
            "PYTHONPATH": py_path,
        }

        # 跨平台：直接用当前解释器启动（deploy 内也走这条路径）
        launcher = bin_dir / f"start-{spec.key}.json"
        launcher.write_text(
            json.dumps(
                {
                    "cwd": service_dir.as_posix(),
                    "executable": sys.executable,
                    "args": ["server.py"],
                    "env": env_exports,
                },
                ensure_ascii=False,
                indent=2,
            )
            + "\n",
            encoding="utf-8",
        )

        sh = bin_dir / f"start-{spec.key}.sh"
        sh_lines = ["#!/usr/bin/env bash", "set -euo pipefail"]
        for k, v in env_exports.items():
            sh_lines.append(f'export {k}="{v}"')
        sh_lines.append(f'cd "{service_dir.as_posix()}"')
        sh_lines.append(f'exec "{sys.executable}" server.py')
        sh.write_text("\n".join(sh_lines) + "\n", encoding="utf-8")
        try:
            sh.chmod(0o755)
        except OSError:
            pass

        ps1 = bin_dir / f"start-{spec.key}.ps1"
        ps_lines = [f'$env:{k} = "{v}"' for k, v in env_exports.items()]
        ps_lines.append(f'Set-Location "{service_dir}"')
        ps_lines.append(f'& "{sys.executable}" server.py')
        ps1.write_text("\n".join(ps_lines) + "\n", encoding="utf-8")
        print(f"启动脚本: {sh} / {ps1}")


def _start_python_service(data_dir: Path, spec: ModelSpec, log_file: Path) -> subprocess.Popen:
    meta_path = data_dir / "bin" / f"start-{spec.key}.json"
    if not meta_path.exists():
        raise FileNotFoundError(f"缺少启动元数据: {meta_path}")
    meta = json.loads(meta_path.read_text(encoding="utf-8"))
    env = os.environ.copy()
    env.update(meta.get("env") or {})
    ensure_dir(log_file.parent)
    logfh = log_file.open("a", encoding="utf-8")
    proc = subprocess.Popen(
        [meta["executable"], *meta.get("args", ["server.py"])],
        cwd=meta.get("cwd") or None,
        env=env,
        stdout=logfh,
        stderr=subprocess.STDOUT,
        start_new_session=True,
    )
    # 把句柄交给进程；关闭由 GC/进程结束处理
    return proc


def _wait_http_ok(url: str, timeout_sec: float = 120.0, interval: float = 2.0) -> bool:
    import urllib.error
    import urllib.request

    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=3) as resp:
                if 200 <= getattr(resp, "status", 200) < 300:
                    body = resp.read().decode("utf-8", errors="replace")
                    # 支持 {"status":"ok","ready":true}；无 ready 字段则兼容旧行为
                    try:
                        payload = json.loads(body)
                        if isinstance(payload, dict) and "ready" in payload:
                            if payload.get("ready") is True:
                                return True
                        else:
                            return True
                    except json.JSONDecodeError:
                        return True
        except (urllib.error.URLError, TimeoutError, OSError):
            pass
        time.sleep(interval)
    return False


def _generate_systemd(data_dir: Path, specs: List[ModelSpec], install: bool) -> None:
    systemd_dst = ensure_dir(data_dir / "systemd")
    for spec in specs:
        if not spec.enabled or spec.engine != "python":
            continue
        unit_name = f"{spec.key}.service"
        text = render_systemd_unit(spec, data_dir)
        (systemd_dst / unit_name).write_text(text, encoding="utf-8")
        if install:
            dest = Path("/etc/systemd/system") / unit_name
            try:
                dest.write_text(text, encoding="utf-8")
                print(f"已写入 {dest}")
            except PermissionError:
                eprint(f"无权限写入 {dest}，已保留 {systemd_dst / unit_name}")
    if install:
        run_cmd(["systemctl", "daemon-reload"], check=False)


def cmd_deploy(args: argparse.Namespace) -> int:
    if not is_air_gapped():
        eprint("警告: 建议设置 AIR_GAPPED_MODE=true 后再部署，避免意外外连。")

    work = Path(args.work_dir or tempfile.mkdtemp(prefix="model-deploy-")).resolve()
    bundle = Path(args.bundle).resolve()
    extracted = _extract_bundle(bundle, work)

    packed_cfg = extracted / "config.yaml"
    cli_cfg = resolve_config_path(args.config)
    if packed_cfg.exists():
        cfg = load_config(packed_cfg, prefer_as_base=True)
        print(f"使用包内配置: {packed_cfg}")
        if cli_cfg and cli_cfg.exists():
            override = load_yaml(cli_cfg)
            override.pop("models_replace", None)
            cfg = deep_merge(cfg, override)
            print(f"叠加覆盖配置: {cli_cfg}")
    elif cli_cfg and cli_cfg.exists():
        cfg = load_config(cli_cfg)
        print(f"使用显式配置: {cli_cfg}")
    else:
        cfg = load_config(cli_cfg)
        print("使用默认/示例配置")

    if args.data_dir:
        cfg.setdefault("deploy", {})["data_dir"] = args.data_dir
    if args.host:
        cfg.setdefault("deploy", {})["host"] = args.host

    data_dir = Path((cfg.get("deploy") or {}).get("data_dir") or "/mnt/models").resolve()
    ensure_dir(data_dir)
    specs = parse_models(cfg)
    enabled = [s for s in specs if s.enabled]

    print("\n=== 安装模型权重并校验 ===")
    src_models = extracted / "models"
    algo = str((cfg.get("bundle") or {}).get("checksum_algo") or "sha256")
    for spec in enabled:
        src = src_models / spec.local_dirname
        dst = model_host_path(data_dir, spec)
        if not src.exists():
            raise FileNotFoundError(f"包内缺少模型目录: {src}")
        print(f"同步 {spec.key}: {src} -> {dst}")
        if dst.exists():
            shutil.rmtree(dst)
        ensure_dir(dst.parent)
        shutil.copytree(src, dst)
        ok, errors = verify_checksums(dst, algo=algo)
        if errors:
            for err in errors:
                eprint(f"  [FAIL] {err}")
            raise RuntimeError(f"模型校验失败: {spec.key}（{len(errors)} 个错误）")
        print(f"  校验通过: {ok} 个文件")

    # 加载全部镜像 tar
    if args.skip_docker:
        print("跳过 docker load")
    else:
        print("\n=== docker load 全部镜像 ===")
        docker_cfg = cfg.get("docker") or {}
        tar_candidates = [
            extracted / (docker_cfg.get("vllm_image_tar") or "images/vllm-openai.tar"),
        ]
        for key in ("rerank", "asr", "tts"):
            tar_candidates.append(extracted / service_image_tar(cfg, key))
        images_dir = extracted / "images"
        if images_dir.exists():
            tar_candidates.extend(sorted(images_dir.glob("*.tar")))
        loaded = set()
        for tar_path in tar_candidates:
            tar_path = tar_path.resolve()
            if tar_path in loaded or not tar_path.exists():
                continue
            print(f"docker load -i {tar_path}")
            run_cmd(["docker", "load", "-i", str(tar_path)])
            loaded.add(tar_path)
        if not loaded:
            eprint("警告: 未找到任何镜像 tar，compose 启动可能失败")

    print("\n=== 端口检查 ===")
    for spec in enabled:
        if port_in_use(spec.port):
            eprint(f"端口冲突: {spec.port} 已被占用（模型 {spec.key}）")
            if not args.ignore_port_conflict:
                raise RuntimeError(
                    f"端口 {spec.port} 冲突。请修改 config 或停止占用进程，或使用 --ignore-port-conflict"
                )

    print("\n=== 生成 compose 与平台登记文件 ===")
    compose_text = render_compose(cfg, data_dir, specs)
    import yaml

    yaml.safe_load(compose_text)
    compose_path = data_dir / "docker-compose.airgap.override.yml"
    compose_path.write_text(compose_text, encoding="utf-8")
    (extracted / "docker-compose.airgap.override.yml").write_text(compose_text, encoding="utf-8")

    registry = render_approved_endpoints(cfg, specs)
    reg_dir = ensure_dir(data_dir / "registry")
    (reg_dir / "approved_endpoints.json").write_text(
        json.dumps(registry, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    dump_yaml(reg_dir / "model_registry.yaml", {"models": registry["model_registry"]})
    dump_yaml(data_dir / "config.yaml", cfg)

    auto_start = bool((cfg.get("deploy") or {}).get("auto_start", True)) and not args.no_start
    project = str((cfg.get("deploy") or {}).get("compose_project") or "weknora-models")
    if auto_start:
        print("\n=== 一键启动全部模型服务 (docker compose) ===")
        run_cmd(
            [
                "docker",
                "compose",
                "-p",
                project,
                "-f",
                str(compose_path),
                "up",
                "-d",
            ]
        )
        print("\n=== 等待健康检查 ===")
        for spec in enabled:
            if not (is_vllm_engine(spec.engine) or is_container_engine(spec.engine)):
                eprint(f"  跳过未知 engine={spec.engine} 的健康检查: {spec.key}")
                continue
            if is_vllm_engine(spec.engine):
                ready_url = f"http://127.0.0.1:{spec.port}/v1/models"
            else:
                ready_url = f"http://127.0.0.1:{spec.port}/healthz"
            if _wait_http_ok(ready_url, timeout_sec=300):
                print(f"  {spec.key} 就绪: {ready_url}")
            else:
                eprint(f"  警告: {spec.key} 超时未就绪: {ready_url}")
                eprint(f"  请检查: docker compose -p {project} -f {compose_path} logs {spec.key}")
    else:
        print("跳过自动启动。一键启动命令:")
        print(f"  docker compose -p {project} -f {compose_path} up -d")

    print(f"\ndeploy 完成。登记文件: {reg_dir}")
    print(f"OpenAI 兼容基址示例: http://{(cfg.get('deploy') or {}).get('host')}:<port>/v1")
    return 0


def cmd_verify(args: argparse.Namespace) -> int:
    cfg = load_config(resolve_config_path(args.config))
    if args.data_dir:
        cfg.setdefault("deploy", {})["data_dir"] = args.data_dir
    data_dir = Path((cfg.get("deploy") or {}).get("data_dir") or "/mnt/models").resolve()
    algo = str((cfg.get("bundle") or {}).get("checksum_algo") or "sha256")
    specs = [s for s in parse_models(cfg) if s.enabled]

    failed = False
    for spec in specs:
        root = model_host_path(data_dir, spec)
        print(f"校验 {spec.key}: {root}")
        if not root.exists():
            eprint("  缺失目录")
            failed = True
            continue
        try:
            ok, errors = verify_checksums(root, algo=algo)
        except FileNotFoundError as exc:
            eprint(f"  {exc}")
            failed = True
            continue
        if errors:
            failed = True
            for err in errors:
                eprint(f"  [FAIL] {err}")
        else:
            print(f"  OK ({ok} files)")
        if port_in_use(spec.port):
            print(f"  端口 {spec.port}: 已监听（服务可能在运行）")
        else:
            print(f"  端口 {spec.port}: 空闲")

    if failed:
        raise RuntimeError("校验未通过")
    print("全部校验通过")
    return 0


def cmd_gen_config(args: argparse.Namespace) -> int:
    cfg = load_config(resolve_config_path(args.config))
    if args.data_dir:
        cfg.setdefault("deploy", {})["data_dir"] = args.data_dir
    if args.host:
        cfg.setdefault("deploy", {})["host"] = args.host
    data_dir = Path((cfg.get("deploy") or {}).get("data_dir") or "/mnt/models")
    out = Path(args.output_dir or ".").resolve()
    ensure_dir(out)
    specs = parse_models(cfg)
    compose_text = render_compose(cfg, data_dir, specs)
    import yaml

    yaml.safe_load(compose_text)  # 确保可解析
    (out / "docker-compose.airgap.override.yml").write_text(compose_text, encoding="utf-8")
    registry = render_approved_endpoints(cfg, specs)
    (out / "approved_endpoints.json").write_text(
        json.dumps(registry, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    dump_yaml(out / "model_registry.yaml", {"models": registry["model_registry"]})
    print(f"已生成到 {out}")
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="内网离线 AI 模型准备与部署工具",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    config_parent = argparse.ArgumentParser(add_help=False)
    config_parent.add_argument(
        "--config",
        default=None,
        help="配置文件路径；默认自动使用 model-scripts/config.yaml（若存在）",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    p_prep = sub.add_parser(
        "prepare",
        parents=[config_parent],
        help="联网机器：下载模型/镜像/依赖并打包",
    )
    p_prep.add_argument("--output-dir", help="输出目录")
    p_prep.add_argument("--clean", action="store_true", help="清空输出目录后重建")
    p_prep.add_argument("--skip-docker", action="store_true", help="跳过全部 docker pull/build/save")
    p_prep.add_argument(
        "--skip-docker-build",
        action="store_true",
        help="仍拉取 vLLM，但跳过 rerank/asr/tts 镜像构建",
    )
    p_prep.add_argument("--skip-pip", action="store_true", help="跳过工具 pip download")
    p_prep.add_argument("--no-archive", action="store_true", help="不生成 tar.gz")
    p_prep.set_defaults(func=cmd_prepare)

    p_dep = sub.add_parser(
        "deploy",
        parents=[config_parent],
        help="内网机器：解压、校验、load 镜像并 compose 一键启动",
    )
    p_dep.add_argument("--bundle", required=True, help="部署包 .tar.gz / .zip 或已解压目录")
    p_dep.add_argument("--data-dir", help="模型与运行数据目录")
    p_dep.add_argument("--host", help="覆盖 deploy.host（写入登记文件）")
    p_dep.add_argument("--work-dir", help="解压工作目录")
    p_dep.add_argument("--skip-docker", action="store_true")
    p_dep.add_argument("--no-start", action="store_true", help="只安装不启动")
    p_dep.add_argument("--ignore-port-conflict", action="store_true")
    p_dep.set_defaults(func=cmd_deploy)

    p_ver = sub.add_parser("verify", parents=[config_parent], help="校验模型文件完整性")
    p_ver.add_argument("--data-dir", help="模型数据目录")
    p_ver.set_defaults(func=cmd_verify)

    p_gen = sub.add_parser("gen-config", parents=[config_parent], help="仅生成 compose 与登记文件")
    p_gen.add_argument("--output-dir", default="./generated")
    p_gen.add_argument("--data-dir")
    p_gen.add_argument("--host")
    p_gen.set_defaults(func=cmd_gen_config)

    return parser


def _extract_global_config_argv(argv: List[str]) -> Tuple[Optional[str], List[str]]:
    """允许 --config 出现在子命令之前或之后。"""
    config: Optional[str] = None
    cleaned: List[str] = []
    i = 0
    while i < len(argv):
        arg = argv[i]
        if arg == "--config" and i + 1 < len(argv):
            config = argv[i + 1]
            i += 2
            continue
        if arg.startswith("--config="):
            config = arg.split("=", 1)[1]
            i += 1
            continue
        cleaned.append(arg)
        i += 1
    return config, cleaned


def main(argv: Optional[List[str]] = None) -> int:
    raw = list(sys.argv[1:] if argv is None else argv)
    pre_config, cleaned = _extract_global_config_argv(raw)
    parser = build_parser()
    args = parser.parse_args(cleaned)
    if pre_config is not None:
        args.config = pre_config
    elif getattr(args, "config", None) is None:
        args.config = None
    try:
        return int(args.func(args))
    except KeyboardInterrupt:
        eprint("已中断")
        return 130
    except Exception as exc:  # noqa: BLE001
        eprint(f"错误: {exc}")
        return 1


if __name__ == "__main__":
    sys.exit(main())
