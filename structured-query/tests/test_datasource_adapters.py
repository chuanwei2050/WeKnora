from pydantic import SecretStr

from structured_query.config import Settings
from structured_query.datasource_adapters import MySQLAdapter, PostgreSQLAdapter, TableRef, create_adapter


def test_factory_resolves_only_server_side_secret(configured_environment, monkeypatch):
    from structured_query import datasource_adapters
    from structured_query.config import get_settings

    settings = get_settings().model_copy(
        update={"datasource_secrets": {"tenant-a/hr": SecretStr("postgresql+psycopg://u:p@db/hr")}}
    )
    captured = []
    monkeypatch.setattr(datasource_adapters, "create_engine", lambda dsn, **kwargs: captured.append((dsn, kwargs)) or object())
    adapter = create_adapter("postgresql", "tenant-a/hr", {TableRef("public", "people")}, settings)
    assert isinstance(adapter, PostgreSQLAdapter)
    assert captured[0][0].startswith("postgresql+")
    assert captured[0][1]["hide_parameters"] is True


def test_mysql_factory_and_empty_whitelist_rejection(configured_environment, monkeypatch):
    from structured_query import datasource_adapters
    from structured_query.config import get_settings

    settings = get_settings().model_copy(
        update={"datasource_secrets": {"tenant-a/mysql": SecretStr("mysql+pymysql://u:p@db/hr")}}
    )
    monkeypatch.setattr(datasource_adapters, "create_engine", lambda *args, **kwargs: object())
    adapter = create_adapter("mysql", "tenant-a/mysql", {TableRef("hr", "people")}, settings)
    assert isinstance(adapter, MySQLAdapter)
    try:
        create_adapter("mysql", "tenant-a/mysql", set(), settings)
    except ValueError as error:
        assert str(error) == "allowed_tables_required"
    else:
        raise AssertionError("empty allowlist must fail closed")


def test_factory_reuses_engine_pool_for_same_datasource(configured_environment, monkeypatch):
    from structured_query import datasource_adapters
    from structured_query.config import get_settings

    datasource_adapters._datasource_engine.cache_clear()
    settings = get_settings().model_copy(
        update={"datasource_secrets": {"tenant-a/shared": SecretStr("postgresql+psycopg://u:p@db/shared")}}
    )
    created = []
    monkeypatch.setattr(
        datasource_adapters,
        "create_engine",
        lambda *_args, **_kwargs: created.append(object()) or created[-1],
    )
    first = create_adapter("postgresql", "tenant-a/shared", {TableRef("public", "people")}, settings)
    second = create_adapter("postgresql", "tenant-a/shared", {TableRef("public", "orders")}, settings)

    assert first.engine is second.engine
    assert len(created) == 1
    datasource_adapters._datasource_engine.cache_clear()
