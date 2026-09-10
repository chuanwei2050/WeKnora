import pytest

from structured_query.config import Settings, postgres_dsn_from_environ, resolve_postgres_dsn


def test_component_credentials_build_encoded_connection_urls(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.delenv("STRUCTURED_QUERY_POSTGRES_DSN", raising=False)
    monkeypatch.delenv("STRUCTURED_QUERY_REDIS_URL", raising=False)
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


def test_resolve_postgres_dsn_from_components():
    assert resolve_postgres_dsn(
        None,
        host="postgres",
        port=5432,
        user="app",
        password="p@ss:#",
        database="We Knora",
    ) == "postgresql+psycopg://app:p%40ss%3A%23@postgres:5432/We%20Knora"


def test_postgres_dsn_from_environ_builds_from_components(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.delenv("STRUCTURED_QUERY_POSTGRES_DSN", raising=False)
    monkeypatch.setenv("STRUCTURED_QUERY_POSTGRES_HOST", "postgres")
    monkeypatch.setenv("STRUCTURED_QUERY_POSTGRES_PORT", "5432")
    monkeypatch.setenv("STRUCTURED_QUERY_POSTGRES_USER", "weknora")
    monkeypatch.setenv("STRUCTURED_QUERY_POSTGRES_PASSWORD", "p@ss:#")
    monkeypatch.setenv("STRUCTURED_QUERY_POSTGRES_DATABASE", "weknora")

    assert postgres_dsn_from_environ() == (
        "postgresql+psycopg://weknora:p%40ss%3A%23@postgres:5432/weknora"
    )


def test_postgres_dsn_from_environ_prefers_explicit_dsn(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv(
        "STRUCTURED_QUERY_POSTGRES_DSN",
        "postgresql+psycopg://explicit:secret@db:5432/weknora",
    )
    monkeypatch.setenv("STRUCTURED_QUERY_POSTGRES_USER", "ignored")
    monkeypatch.setenv("STRUCTURED_QUERY_POSTGRES_DATABASE", "ignored")

    assert postgres_dsn_from_environ() == "postgresql+psycopg://explicit:secret@db:5432/weknora"
