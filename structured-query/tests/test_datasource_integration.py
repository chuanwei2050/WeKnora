import os

import pytest
from sqlalchemy import create_engine, text

from structured_query.config import get_settings
from structured_query.datasource_adapters import MySQLAdapter, PostgreSQLAdapter, TableRef
from structured_query.execution import QueryExecutionError


@pytest.mark.parametrize(
    ("env_name", "adapter_type", "table", "create_sql", "select_sql", "write_sql"),
    [
        (
            "STRUCTURED_QUERY_TEST_POSTGRES_DSN",
            PostgreSQLAdapter,
            TableRef("public", "people"),
            'CREATE TABLE people (name text, degree text)',
            'SELECT COUNT(*) AS total FROM "public"."people"',
            "INSERT INTO people VALUES ('x', 'y')",
        ),
        (
            "STRUCTURED_QUERY_TEST_MYSQL_DSN",
            MySQLAdapter,
            TableRef("structured_test", "people"),
            "CREATE TABLE people (name varchar(64), degree varchar(64))",
            "SELECT COUNT(*) AS total FROM `structured_test`.`people`",
            "INSERT INTO people VALUES ('x', 'y')",
        ),
    ],
)
def test_real_adapter_inspection_read_only_execution(
    env_name, adapter_type, table, create_sql, select_sql, write_sql
):
    dsn = os.environ.get(env_name)
    if not dsn:
        pytest.skip(f"{env_name} is not configured")
    admin = create_engine(dsn)
    with admin.begin() as connection:
        connection.execute(text("DROP TABLE IF EXISTS people"))
        connection.execute(text(create_sql))
        connection.execute(text("INSERT INTO people VALUES ('张三', '硕士'), ('李四', '本科')"))
    adapter = adapter_type(dsn, {table}, get_settings())
    profiles = adapter.inspect_tables()
    assert len(profiles) == 1
    assert [column["name"] for column in profiles[0].columns] == ["name", "degree"]
    visible = adapter.profile_column(table, "degree", True)
    hidden = adapter.profile_column(table, "degree", False)
    assert visible["distinct_count"] == 2 and {item["value"] for item in visible["values"]} == {"硕士", "本科"}
    assert hidden["distinct_count"] == 2 and hidden["values"] == [] and hidden["values_persisted"] is False
    assert adapter.execute(select_sql).rows == [[2]]
    with pytest.raises(QueryExecutionError):
        adapter.execute(write_sql)
    with admin.begin() as connection:
        assert connection.execute(text("SELECT COUNT(*) FROM people")).scalar_one() == 2
        connection.execute(text("DROP TABLE people"))
    admin.dispose()
