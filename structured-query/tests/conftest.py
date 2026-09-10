import os

import pytest
from fastapi.testclient import TestClient


@pytest.fixture(autouse=True)
def configured_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    values = {
        "POSTGRES_DSN": "postgresql+psycopg://u:p@postgres/db",
        "REDIS_URL": "redis://redis/2",
        "S3_ENDPOINT": "http://minio:9000",
        "S3_ACCESS_KEY": "test",
        "S3_SECRET_KEY": "test",
        "MILVUS_URI": "http://milvus:19530",
        "WEKNORA_BASE_URL": "http://weknora",
        "WEKNORA_SERVICE_KEY": "test-service-key",
        "API_KEYS": "valid-key:tenant-a",
    }
    for key, value in values.items():
        monkeypatch.setenv(f"STRUCTURED_QUERY_{key}", value)
    from structured_query.config import get_settings
    from structured_query.model_config import clear_runtime_model_config_cache

    get_settings.cache_clear()
    clear_runtime_model_config_cache()
    yield
    clear_runtime_model_config_cache()
    get_settings.cache_clear()


@pytest.fixture
def client() -> TestClient:
    from structured_query.api import create_app

    return TestClient(create_app(initialize=False))
