"""
OpenAI 兼容 TTS 服务（FunAudioLLM/CosyVoice2-0.5B）

POST /v1/audio/speech
  JSON: {"model","input","voice","response_format"}
GET  /v1/models

WeKnora 默认 response_format=mp3，本服务支持 mp3|wav|pcm|opus(降级为 wav)。
"""

from __future__ import annotations

import io
import os
from pathlib import Path
from typing import Any, Dict, Optional, Tuple

import numpy as np
from fastapi import FastAPI, HTTPException
from fastapi.responses import Response
from pydantic import BaseModel, Field

MODEL_DIR = Path(os.environ.get("MODEL_DIR", "./model"))
SERVED_MODEL_NAME = os.environ.get("SERVED_MODEL_NAME", "model")
PORT = int(os.environ.get("PORT", "8005"))
QUANT = os.environ.get("QUANT", "fp16")
PROMPT_WAV = Path(
    os.environ.get("PROMPT_WAV", "/opt/CosyVoice/asset/zero_shot_prompt.wav")
)
PROMPT_TEXT = os.environ.get("PROMPT_TEXT", "希望你以后能够做的比我还好呦。")

app = FastAPI(title="Airgap TTS", version="1.0.0")
_tts = None
_load_error: Optional[str] = None


class SpeechRequest(BaseModel):
    model: Optional[str] = None
    input: str = Field(..., min_length=1)
    voice: str = "default"
    response_format: str = "mp3"
    speed: float = 1.0


def _cuda_available() -> bool:
    try:
        import torch

        return torch.cuda.is_available()
    except Exception:  # noqa: BLE001
        return False


def _block_runtime_model_downloads() -> None:
    if os.environ.get("AIR_GAPPED_MODE", "").lower() not in {"1", "true", "yes"}:
        return
    try:
        import modelscope
    except ImportError:
        return

    def blocked_snapshot_download(*args: Any, **kwargs: Any) -> None:
        raise RuntimeError("AIR_GAPPED_MODE=true 禁止 TTS 运行时下载模型或文本前端资源")

    modelscope.snapshot_download = blocked_snapshot_download


def _load() -> None:
    global _tts, _load_error
    if _tts is not None:
        return
    if not MODEL_DIR.exists():
        raise RuntimeError(f"MODEL_DIR 不存在: {MODEL_DIR}")
    _block_runtime_model_downloads()

    errors = []
    for import_path, cls_name in (
        ("cosyvoice.cli.cosyvoice", "CosyVoice2"),
        ("cosyvoice.cli.cosyvoice", "CosyVoice"),
    ):
        try:
            mod = __import__(import_path, fromlist=[cls_name])
            cls = getattr(mod, cls_name)
            _tts = cls(str(MODEL_DIR))
            _load_error = None
            return
        except Exception as exc:  # noqa: BLE001
            errors.append(f"{import_path}.{cls_name}: {exc}")

    raise RuntimeError(
        "无法加载 CosyVoice。请确认 prepare 已拉取 vendor/CosyVoice 且 PYTHONPATH 已包含该目录。"
        f" 尝试错误: {'; '.join(errors)}"
    )


def _synthesize(text: str, voice: str) -> Tuple[np.ndarray, int]:
    _load()
    available_spks = list(_tts.list_available_spks()) if hasattr(_tts, "list_available_spks") else []
    selected_voice = voice if voice in available_spks else None
    if selected_voice is None and voice == "default" and available_spks:
        selected_voice = available_spks[0]

    if selected_voice and hasattr(_tts, "inference_sft"):
        gen = _tts.inference_sft(text, selected_voice)
    elif hasattr(_tts, "inference_zero_shot") and PROMPT_WAV.is_file() and PROMPT_TEXT:
        gen = _tts.inference_zero_shot(text, PROMPT_TEXT, str(PROMPT_WAV))
    else:
        raise RuntimeError(
            "TTS 没有可用预置音色，且 zero-shot prompt 未配置或文件不存在: "
            f"{PROMPT_WAV}"
        )

    chunks = []
    sample_rate = 22050
    for item in gen:
        if isinstance(item, dict):
            wav = item.get("tts_speech")
            sample_rate = int(item.get("sample_rate") or sample_rate)
        else:
            wav = item
        if hasattr(wav, "detach"):
            arr = wav.detach().cpu().numpy()
        else:
            arr = np.asarray(wav)
        chunks.append(arr.reshape(-1))
    if not chunks:
        raise RuntimeError("TTS 未产生音频")
    audio = np.concatenate(chunks).astype(np.float32)
    return audio, sample_rate


def _to_wav_bytes(audio: np.ndarray, sr: int) -> bytes:
    import soundfile as sf

    buf = io.BytesIO()
    sf.write(buf, audio, sr, format="WAV")
    return buf.getvalue()


def _to_pcm_bytes(audio: np.ndarray) -> bytes:
    return (np.clip(audio, -1, 1) * 32767.0).astype(np.int16).tobytes()


def _to_mp3_bytes(audio: np.ndarray, sr: int) -> bytes:
    """优先 lameenc（纯 Python wheel，适合离线）；其次 pydub+ffmpeg。"""
    try:
        import lameenc

        pcm = _to_pcm_bytes(audio)
        encoder = lameenc.Encoder()
        encoder.set_bit_rate(128)
        encoder.set_in_sample_rate(sr)
        encoder.set_channels(1)
        encoder.set_quality(2)
        return bytes(encoder.encode(pcm) + encoder.flush())
    except Exception as lame_err:  # noqa: BLE001
        try:
            from pydub import AudioSegment

            wav_buf = io.BytesIO(_to_wav_bytes(audio, sr))
            seg = AudioSegment.from_file(wav_buf, format="wav")
            out = io.BytesIO()
            seg.export(out, format="mp3")
            return out.getvalue()
        except Exception as pydub_err:  # noqa: BLE001
            raise RuntimeError(
                "无法编码 mp3。已尝试 lameenc 与 pydub+ffmpeg 均失败。"
                "请确认 offline_packages 含 lameenc，或安装 ffmpeg；"
                "亦可将 WeKnora TTS response_format 设为 wav。"
                f" lameenc={lame_err}; pydub={pydub_err}"
            ) from pydub_err


@app.on_event("startup")
def startup() -> None:
    global _load_error
    try:
        _load()
    except Exception as exc:  # noqa: BLE001
        _load_error = str(exc)
        print(f"[warn] TTS 预加载失败（可延后到首请求）: {exc}")


@app.get("/healthz")
def healthz() -> Dict[str, Any]:
    ready = _tts is not None and _load_error is None
    payload: Dict[str, Any] = {
        "status": "ok" if ready else "degraded",
        "ready": ready,
        "quant": QUANT,
        "cuda": _cuda_available(),
    }
    if _load_error:
        payload["error"] = _load_error
    return payload


@app.get("/v1/models")
def list_models() -> Dict[str, Any]:
    return {
        "object": "list",
        "data": [{"id": SERVED_MODEL_NAME, "object": "model", "owned_by": "airgap"}],
    }


@app.post("/v1/audio/speech")
def speech(req: SpeechRequest) -> Response:
    fmt = (req.response_format or "mp3").lower().strip()
    # opus 暂无编码器时降级 wav，避免平台硬失败
    if fmt == "opus":
        fmt = "wav"
    if fmt not in {"wav", "pcm", "mp3"}:
        raise HTTPException(status_code=400, detail="支持 response_format=mp3|wav|pcm")
    try:
        audio, sr = _synthesize(req.input, req.voice)
    except Exception as exc:  # noqa: BLE001
        raise HTTPException(status_code=500, detail=str(exc)) from exc

    if fmt == "pcm":
        return Response(content=_to_pcm_bytes(audio), media_type="application/octet-stream")
    if fmt == "mp3":
        try:
            body = _to_mp3_bytes(audio, sr)
        except Exception as exc:  # noqa: BLE001
            raise HTTPException(status_code=500, detail=str(exc)) from exc
        return Response(content=body, media_type="audio/mpeg")
    return Response(content=_to_wav_bytes(audio, sr), media_type="audio/wav")


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=PORT)
