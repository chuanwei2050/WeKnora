from dataclasses import dataclass
from threading import Lock
from time import monotonic

import httpx
from pydantic import BaseModel, ConfigDict, Field

from .config import get_settings


class RuntimeModel(BaseModel):
    model_config = ConfigDict(extra="ignore")

    id: str
    name: str
    base_url: str
    api_key: str
    dimension: int = Field(default=0, ge=0, le=65536)


class RuntimeModelConfig(BaseModel):
    model_config = ConfigDict(extra="ignore")

    chat: RuntimeModel
    embedding: RuntimeModel


@dataclass(frozen=True)
class _CacheEntry:
    expires_at: float
    config: RuntimeModelConfig


_cache: dict[str, _CacheEntry] = {}
_lock = Lock()


def get_runtime_model_config(tenant_id: str) -> RuntimeModelConfig:
    settings = get_settings()
    now = monotonic()
    with _lock:
        cached = _cache.get(tenant_id)
        if cached is not None and cached.expires_at > now:
            return cached.config
    response = httpx.get(
        f"{settings.weknora_base_url.rstrip('/')}/api/internal/v1/structured-query/model-config",
        params={"tenant_id": tenant_id},
        headers={"X-Structured-Query-Key": settings.weknora_service_key.get_secret_value()},
        timeout=10.0,
    )
    response.raise_for_status()
    resolved = RuntimeModelConfig.model_validate(response.json())
    if not resolved.chat.name or not resolved.chat.base_url:
        raise ValueError("chat_model_config_incomplete")
    if not resolved.embedding.name or not resolved.embedding.base_url or resolved.embedding.dimension <= 0:
        raise ValueError("embedding_model_config_incomplete")
    with _lock:
        _cache[tenant_id] = _CacheEntry(now + settings.model_config_ttl_seconds, resolved)
    return resolved


def clear_runtime_model_config_cache() -> None:
    with _lock:
        _cache.clear()
