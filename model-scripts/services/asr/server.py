"""
OpenAI 兼容 ASR 服务（FunAudioLLM/SenseVoiceSmall）

POST /v1/audio/transcriptions
GET  /v1/models
"""

from __future__ import annotations

import os
import tempfile
from pathlib import Path
from typing import Any, Dict, Optional

from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from fastapi.responses import JSONResponse, PlainTextResponse

MODEL_DIR = Path(os.environ.get("MODEL_DIR", "./model"))
SERVED_MODEL_NAME = os.environ.get("SERVED_MODEL_NAME", "sensevoice-small")
PORT = int(os.environ.get("PORT", "8004"))
QUANT = os.environ.get("QUANT", "fp16")

app = FastAPI(title="Airgap ASR", version="1.0.0")
_asr = None
_load_error: Optional[str] = None


def _cuda_available() -> bool:
    try:
        import torch

        return torch.cuda.is_available()
    except Exception:  # noqa: BLE001
        return False


def _load() -> None:
    global _asr, _load_error
    if _asr is not None:
        return
    if not MODEL_DIR.exists():
        raise RuntimeError(f"MODEL_DIR 不存在: {MODEL_DIR}")
    from funasr import AutoModel

    _asr = AutoModel(
        model=str(MODEL_DIR),
        trust_remote_code=True,
        disable_update=True,
        device="cuda" if _cuda_available() else "cpu",
    )
    _load_error = None


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
    payload: Dict[str, Any] = {"status": "ok" if ready else "degraded", "ready": ready, "quant": QUANT}
    if _load_error:
        payload["error"] = _load_error
    return payload


@app.get("/v1/models")
def list_models() -> Dict[str, Any]:
    return {
        "object": "list",
        "data": [{"id": SERVED_MODEL_NAME, "object": "model", "owned_by": "airgap"}],
    }


@app.post("/v1/audio/transcriptions")
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
        kwargs: Dict[str, Any] = {"input": tmp_path}
        if language:
            kwargs["language"] = language
        result = _asr.generate(**kwargs)
        text = ""
        if isinstance(result, list) and result:
            item = result[0]
            text = item.get("text") if isinstance(item, dict) else str(item)
        elif isinstance(result, dict):
            text = str(result.get("text") or result)
        else:
            text = str(result)
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
