"""
OpenAI 兼容 ASR 服务

POST /v1/audio/transcriptions
GET  /v1/models

QUANT=onnx / onnx-int8 时优先 funasr-onnx + model_quant.onnx（体积更小、CPU 更快）；
否则走 FunASR AutoModel（官方 PyTorch 权重）。
"""

from __future__ import annotations

import os
import secrets
import tempfile
from pathlib import Path
from typing import Any, Dict, Optional

from fastapi import Depends, FastAPI, File, Form, Header, HTTPException, UploadFile
from fastapi.responses import JSONResponse, PlainTextResponse

MODEL_DIR = Path(os.environ.get("MODEL_DIR", "./model"))
SERVED_MODEL_NAME = os.environ.get("SERVED_MODEL_NAME", "model")
PORT = int(os.environ.get("PORT", "8004"))
QUANT = os.environ.get("QUANT", "fp16")

app = FastAPI(title="Airgap ASR", version="1.0.0")
_asr = None
_backend = "none"
_load_error: Optional[str] = None


def require_api_key(authorization: Optional[str] = Header(None)) -> None:
    """Validate the shared model API key at the HTTP boundary."""
    api_key = os.environ.get("MODEL_API_KEY", "")
    if not api_key:
        raise HTTPException(status_code=503, detail="MODEL_API_KEY 未配置")
    scheme, _, token = (authorization or "").partition(" ")
    if scheme.lower() != "bearer" or not secrets.compare_digest(token, api_key):
        raise HTTPException(
            status_code=401,
            detail="Invalid API key",
            headers={"WWW-Authenticate": "Bearer"},
        )


def _cuda_available() -> bool:
    try:
        import torch

        return torch.cuda.is_available()
    except Exception:  # noqa: BLE001
        return False


def _transcribe_onnx(path: str, language: Optional[str]) -> str:
    from funasr_onnx.utils.postprocess_utils import rich_transcription_postprocess

    lang = language or "auto"
    res = _asr([path], language=lang, use_itn=True)
    if not res:
        return ""
    first = res[0]
    if isinstance(first, str):
        try:
            return rich_transcription_postprocess(first)
        except Exception:  # noqa: BLE001
            return first
    if isinstance(first, dict):
        text = str(first.get("text") or first)
        try:
            return rich_transcription_postprocess(text)
        except Exception:  # noqa: BLE001
            return text
    return str(first)


def _transcribe_funasr(path: str, language: Optional[str]) -> str:
    kwargs: Dict[str, Any] = {"input": path}
    if language:
        kwargs["language"] = language
    result = _asr.generate(**kwargs)
    if isinstance(result, list) and result:
        item = result[0]
        return item.get("text") if isinstance(item, dict) else str(item)
    if isinstance(result, dict):
        return str(result.get("text") or result)
    return str(result)


def _load() -> None:
    global _asr, _backend, _load_error
    if _asr is not None:
        return
    if not MODEL_DIR.exists():
        raise RuntimeError(f"MODEL_DIR 不存在: {MODEL_DIR}")

    want_onnx = QUANT.lower().startswith("onnx")
    if want_onnx:
        try:
            from funasr_onnx import SenseVoiceSmall

            # quantize=True → 优先 model_quant.onnx（INT8）
            _asr = SenseVoiceSmall(str(MODEL_DIR), batch_size=1, quantize=True)
            _backend = "funasr-onnx"
            _load_error = None
            print(f"[asr] funasr-onnx loaded quant={QUANT}")
            return
        except Exception as onnx_err:  # noqa: BLE001
            raise RuntimeError(
                f"QUANT={QUANT} 但 funasr-onnx 加载失败: {onnx_err}"
            ) from onnx_err

    from funasr import AutoModel

    _asr = AutoModel(
        model=str(MODEL_DIR),
        trust_remote_code=True,
        disable_update=True,
        device="cuda" if _cuda_available() else "cpu",
    )
    _backend = "funasr"
    _load_error = None
    print(f"[asr] FunASR loaded quant={QUANT}")


@app.on_event("startup")
def startup() -> None:
    global _load_error
    try:
        _load()
    except Exception as exc:  # noqa: BLE001
        _load_error = str(exc)
        print(f"[warn] ASR 预加载失败: {exc}")


@app.get("/healthz")
def healthz() -> Dict[str, Any]:
    ready = _asr is not None and _load_error is None
    payload: Dict[str, Any] = {
        "status": "ok" if ready else "degraded",
        "ready": ready,
        "quant": QUANT,
        "backend": _backend,
    }
    if _load_error:
        payload["error"] = _load_error
    return payload


@app.get("/v1/models", dependencies=[Depends(require_api_key)])
def list_models() -> Dict[str, Any]:
    return {
        "object": "list",
        "data": [{"id": SERVED_MODEL_NAME, "object": "model", "owned_by": "airgap"}],
    }


@app.post(
    "/v1/audio/transcriptions",
    dependencies=[Depends(require_api_key)],
)
async def transcriptions(
    file: UploadFile = File(...),
    model: Optional[str] = Form(None),
    language: Optional[str] = Form(None),
    response_format: Optional[str] = Form("json"),
):
    try:
        _load()
    except Exception as exc:  # noqa: BLE001
        raise HTTPException(status_code=500, detail=str(exc)) from exc

    suffix = Path(file.filename or "audio.wav").suffix or ".wav"
    tmp_path = ""
    try:
        with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as tmp:
            tmp.write(await file.read())
            tmp_path = tmp.name
        if _backend == "funasr-onnx":
            text = _transcribe_onnx(tmp_path, language)
        else:
            text = _transcribe_funasr(tmp_path, language)
    except Exception as exc:  # noqa: BLE001
        raise HTTPException(status_code=500, detail=f"ASR 推理失败: {exc}") from exc
    finally:
        if tmp_path:
            try:
                os.unlink(tmp_path)
            except OSError:
                pass

    fmt = (response_format or "json").lower().strip()
    if fmt == "text":
        return PlainTextResponse(content=text)
    if fmt == "verbose_json":
        return JSONResponse(
            content={
                "task": "transcribe",
                "language": language or "zh",
                "duration": None,
                "text": text,
                "segments": [],
                "model": model or SERVED_MODEL_NAME,
            }
        )
    return JSONResponse(content={"text": text, "model": model or SERVED_MODEL_NAME})


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=PORT)
