"""RapidOCR backend (ONNX Runtime) — same stack as FinOpsSys local invoice OCR.

Avoids paddlepaddle native segfaults (fused_conv2d / reshape) under gRPC workers.
"""

from __future__ import annotations

import io
import logging
import os
import threading
from typing import Any, Union

import numpy as np
from PIL import Image

from docreader.ocr.base import OCRBackend

logger = logging.getLogger(__name__)

# Cap concurrent RapidOCR predicts. Default 4 matches ASYNQ_IMAGE_CONCURRENCY.
_DEFAULT_OCR_MAX_CONCURRENT = 4


def _ocr_max_concurrent() -> int:
    raw = os.getenv("OCR_MAX_CONCURRENT", "").strip()
    if not raw:
        raw = os.getenv("DOCREADER_OCR_MAX_CONCURRENT", "").strip()
    if not raw:
        return _DEFAULT_OCR_MAX_CONCURRENT
    try:
        n = int(raw)
    except ValueError:
        return _DEFAULT_OCR_MAX_CONCURRENT
    if n < 1:
        return 1
    if n > 16:
        return 16
    return n


class RapidOCRBackend(OCRBackend):
    """PP-OCR via RapidOCR + ONNX Runtime (no paddlepaddle)."""

    def __init__(self) -> None:
        self._engine: Any | None = None
        limit = _ocr_max_concurrent()
        self._sem = threading.Semaphore(limit)
        self._init_engine()
        logger.info("RapidOCR concurrency limit=%d (OCR_MAX_CONCURRENT)", limit)

    def _init_engine(self) -> None:
        try:
            from rapidocr import RapidOCR
        except ImportError as e:
            logger.error(
                "Failed to import rapidocr: %s. Install with "
                "'pip install rapidocr onnxruntime'",
                e,
            )
            return
        try:
            # Default profile matches FinOpsSys "small-onnx" (CPU-safe).
            self._engine = RapidOCR()
            logger.info("RapidOCR engine initialized successfully (onnx)")
        except Exception as e:
            logger.error("Failed to initialize RapidOCR: %s", e)
            self._engine = None

    def predict(self, image: Union[str, bytes, Image.Image]) -> str:
        if isinstance(image, str):
            image = Image.open(image)
        elif isinstance(image, bytes):
            image = Image.open(io.BytesIO(image))

        if not isinstance(image, Image.Image):
            raise TypeError("image must be a string, bytes, or PIL Image object")

        return self._predict(image)

    def _predict(self, image: Image.Image) -> str:
        if self._engine is None:
            logger.error("RapidOCR engine not initialized")
            return ""
        try:
            if image.mode != "RGB":
                image = image.convert("RGB")
            image_array = np.asarray(image)

            with self._sem:
                output = self._engine(image_array)

            text = self._extract_text(output)
            logger.info("RapidOCR extracted %d characters", len(text))
            return text
        except Exception as e:
            logger.error("RapidOCR recognition error: %s", e)
            return ""

    @staticmethod
    def _extract_text(output: Any) -> str:
        if output is None:
            return ""

        # RapidOCR 3.x: result object with .txts
        txts = getattr(output, "txts", None)
        if txts is not None:
            parts = [str(t).strip() for t in txts if t and str(t).strip()]
            return " ".join(parts)

        # Older tuple/list forms: [[box, (text, score)], ...] or (boxes, texts, scores)
        if isinstance(output, tuple) and len(output) >= 2:
            texts = output[1]
            if texts is None:
                return ""
            if isinstance(texts, (list, tuple)):
                parts = [str(t).strip() for t in texts if t and str(t).strip()]
                return " ".join(parts)

        if isinstance(output, list) and output:
            parts: list[str] = []
            for item in output:
                if isinstance(item, (list, tuple)) and len(item) >= 2:
                    rec = item[1]
                    if isinstance(rec, (list, tuple)) and rec:
                        parts.append(str(rec[0]).strip())
                    elif isinstance(rec, str):
                        parts.append(rec.strip())
            return " ".join(p for p in parts if p)

        return ""
