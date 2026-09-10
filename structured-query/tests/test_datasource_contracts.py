import pytest
from pydantic import ValidationError

from structured_query.contracts import DatabaseDatasetCreate


def test_database_registration_contract_rejects_inline_connection_fields():
    payload = {
        "kind": "database", "namespace": "kb", "name": "hr", "dialect": "mysql",
        "secret_ref": "tenant-a/hr", "catalog": "hr",
        "tables": [{"schema": "hr", "table": "people"}],
        "dsn": "mysql://forbidden", "password": "forbidden",
    }
    with pytest.raises(ValidationError):
        DatabaseDatasetCreate.model_validate(payload)


def test_external_database_values_are_not_persisted_by_default():
    request = DatabaseDatasetCreate.model_validate({
        "kind": "database", "namespace": "kb", "name": "hr", "dialect": "mysql",
        "secret_ref": "tenant-a/hr", "catalog": "hr",
        "tables": [{"schema": "hr", "table": "people"}],
        "field_policies": [{"schema": "hr", "table": "people", "column": "name"}],
    })
    assert request.field_policies[0].persist_values is False
