"""
OpenAI 兼容 Rerank 服务（BAAI/bge-reranker-v2-m3）

环境变量:
  MODEL_DIR, SERVED_MODEL_NAME, PORT, QUANT
"""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any, Dict, List, Optional

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

MODEL_DIR = Path(os.environ.get("MODEL_DIR", "./model"))
SERVED_MODEL_NAME = os.environ.get("SERVED_MODEL_NAME", "bge-reranker-v2-m3")
PORT = int(os.environ.get("PORT", "8002"))
QUANT = os.environ.get("QUANT", "fp16")

app = FastAPI(title="Airgap Rerank", version="1.0.0")
_model = None
_tokenizer = None
_onnx_session = None
_load_error: Optional[str] = None


class RerankRequest(BaseModel):
    model: Optional[str] = None
    query: str
    documents: List[str] = Field(default_factory=list)
    top_n: Optional[int] = None
    return_documents: bool = True


def _load() -> None:
    global _model, _tokenizer, _onnx_session, _load_error
    if _model is not None or _onnx_session is not None:
        return
    if not MODEL_DIR.exists():
        raise RuntimeError(f"MODEL_DIR 不存在: {MODEL_DIR}")

    onnx_candidates = list(MODEL_DIR.rglob("*.onnx"))
    if QUANT.lower().startswith("onnx"):
        if not onnx_candidates:
            raise RuntimeError(
                f"QUANT={QUANT} 但 MODEL_DIR 中未找到 .onnx 文件: {MODEL_DIR}。"
                "请放入 ONNX 权重，或将 config 中 quant 改为 fp16/torch。"
            )
        import onnxruntime as ort
        from transformers import AutoTokenizer

        onnx_path = onnx_candidates[0]
        _onnx_session = ort.InferenceSession(
            str(onnx_path),
            providers=["CUDAExecutionProvider", "CPUExecutionProvider"],
        )
        _tokenizer = AutoTokenizer.from_pretrained(str(MODEL_DIR), trust_remote_code=True)
        _load_error = None
        return

    from transformers import AutoModelForSequenceClassification, AutoTokenizer
    import torch

    _tokenizer = AutoTokenizer.from_pretrained(str(MODEL_DIR), trust_remote_code=True)
    _model = AutoModelForSequenceClassification.from_pretrained(
        str(MODEL_DIR), trust_remote_code=True
    )
    _model.eval()
    if torch.cuda.is_available():
        _model = _model.cuda()
    _load_error = None


def _score_pairs(query: str, documents: List[str]) -> List[float]:
    _load()
    import numpy as np
    import torch

    pairs = [(query, doc) for doc in documents]
    if _onnx_session is not None:
        enc = _tokenizer(
            pairs,
            padding=True,
            truncation=True,
            max_length=512,
            return_tensors="np",
        )
        inputs = {k: v for k, v in enc.items() if k in [i.name for i in _onnx_session.get_inputs()]}
        if not inputs:
            inputs = {
                _onnx_session.get_inputs()[0].name: enc["input_ids"],
                _onnx_session.get_inputs()[1].name: enc["attention_mask"],
            }
        outs = _onnx_session.run(None, inputs)
        logits = np.array(outs[0]).reshape(-1)
        return logits.tolist()

    enc = _tokenizer(
        pairs,
        padding=True,
        truncation=True,
        max_length=512,
        return_tensors="pt",
    )
    if next(_model.parameters()).is_cuda:
        enc = {k: v.cuda() for k, v in enc.items()}
    with torch.no_grad():
        logits = _model(**enc).logits.view(-1).float().cpu().tolist()
    return logits


@app.on_event("startup")
def startup() -> None:
    global _load_error
    try:
        _load()
    except Exception as exc:  # noqa: BLE001
        _load_error = str(exc)
        print(f"[warn] 预加载失败: {exc}")


@app.get("/healthz")
def healthz() -> Dict[str, Any]:
    ready = (_model is not None or _onnx_session is not None) and _load_error is None
    payload: Dict[str, Any] = {"status": "ok" if ready else "degraded", "ready": ready}
    if _load_error:
        payload["error"] = _load_error
    return payload


@app.get("/v1/models")
def list_models() -> Dict[str, Any]:
    return {
        "object": "list",
        "data": [
            {
                "id": SERVED_MODEL_NAME,
                "object": "model",
                "owned_by": "airgap",
            }
        ],
    }


@app.post("/v1/rerank")
def rerank(req: RerankRequest) -> Dict[str, Any]:
    if not req.query or not req.documents:
        raise HTTPException(status_code=400, detail="query 与 documents 必填")
    try:
        scores = _score_pairs(req.query, req.documents)
    except Exception as exc:  # noqa: BLE001
        raise HTTPException(status_code=500, detail=str(exc)) from exc

    ranked = sorted(
        (
            {
                "index": i,
                "relevance_score": float(scores[i]),
                **({"document": {"text": req.documents[i]}} if req.return_documents else {}),
            }
            for i in range(len(req.documents))
        ),
        key=lambda x: x["relevance_score"],
        reverse=True,
    )
    if req.top_n is not None:
        ranked = ranked[: max(0, int(req.top_n))]
    return {
        "id": "rerank-1",
        "model": req.model or SERVED_MODEL_NAME,
        "results": ranked,
    }


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=PORT)
