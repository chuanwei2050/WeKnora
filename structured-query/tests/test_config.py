import pytest

from structured_query.config import Settings


def test_component_credentials_build_encoded_connection_urls(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.delenv("STRUCTURED_QUERY_POSTGRES_DSN")
    monkeypatch.delenv("STRUCTURED_QUERY_REDIS_URL")
    settings = Settings(
        postgres_host="postgres",
        postgres_user="app",
        postgres_password="p@ss:#",
        postgres_database="We Knora",
        redis_host="redis",
        redis_password="r@ss:#",
        s3_endpoint="http://minio:9000",
        s3_access_key="test",
        s3_secret_key="test",
        milvus_uri="http://milvus:19530",
        weknora_base_url="http://app:8080",
        weknora_service_key="service-key",
        api_keys="service-key:service",
    )

    assert settings.postgres_dsn is not None
    assert settings.postgres_dsn.get_secret_value() == (
        "postgresql+psycopg://app:p%40ss%3A%23@postgres:5432/We%20Knora"
    )
    assert settings.redis_url is not None
    assert settings.redis_url.get_secret_value() == "redis://:r%40ss%3A%23@redis:6379/2"
