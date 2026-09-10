import json
import logging
from time import perf_counter
from uuid import uuid4

from prometheus_client import Counter, Histogram

REQUESTS = Counter("structured_query_requests_total", "HTTP requests", ["method", "path", "status"])
REQUEST_SECONDS = Histogram("structured_query_request_seconds", "HTTP request duration", ["method", "path"])
QUERY_SECONDS = Histogram("structured_query_stage_seconds", "Structured query stage duration", ["stage"])
MODEL_CALLS = Counter("structured_query_model_calls_total", "SQL model calls", ["purpose"])

logger = logging.getLogger("structured_query")


async def metrics_middleware(request, call_next):
    started = perf_counter()
    request_id = request.headers.get("x-request-id", uuid4().hex)
    try:
        response = await call_next(request)
        status = response.status_code
        return response
    except Exception:
        status = 500
        raise
    finally:
        elapsed = perf_counter() - started
        REQUESTS.labels(request.method, request.url.path, str(status)).inc()
        REQUEST_SECONDS.labels(request.method, request.url.path).observe(elapsed)
        logger.info(json.dumps({"event": "http_request", "request_id": request_id, "method": request.method, "path": request.url.path, "status": status, "duration_ms": int(elapsed * 1000)}))
