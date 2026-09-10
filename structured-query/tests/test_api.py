from io import BytesIO
from contextlib import nullcontext
from types import SimpleNamespace
from uuid import uuid4


def test_liveness_does_not_require_auth(client):
    response = client.get("/v1/health/live")
    assert response.status_code == 200
    assert response.json() == {"status": "ok", "dependencies": {}}


def test_metrics_endpoint_does_not_expose_configured_secret(client):
    response = client.get("/metrics")
    assert response.status_code == 200
    assert "structured_query_requests_total" in response.text
    assert "valid-key" not in response.text


def test_query_requires_api_key(client):
    response = client.post("/v1/query", json={"namespace": "kb", "question": "人数"})
    assert response.status_code == 401


def test_service_api_key_requires_positive_tenant_header(client, monkeypatch):
    from structured_query.config import get_settings

    settings = get_settings().model_copy(update={"api_keys": {"service-key": "service"}})
    client.app.dependency_overrides[get_settings] = lambda: settings
    try:
        missing = client.post(
            "/v1/query", headers={"X-API-Key": "service-key"},
            json={"namespace": "ns", "question": "人数"},
        )
        invalid = client.post(
            "/v1/query", headers={"X-API-Key": "service-key", "X-Tenant-ID": "tenant-a"},
            json={"namespace": "ns", "question": "人数"},
        )
    finally:
        client.app.dependency_overrides.pop(get_settings, None)

    assert missing.status_code == 401
    assert invalid.status_code == 401


def test_upload_rejects_unsupported_file(client):
    response = client.post(
        "/v1/datasets/files",
        headers={"X-API-Key": "valid-key", "Idempotency-Key": "abcdefgh"},
        data={"namespace": "kb"},
        files={"file": ("data.txt", BytesIO(b"x"), "text/plain")},
    )
    assert response.status_code == 415


def test_upload_accepts_csv_contract(client, monkeypatch):
    from structured_query import api
    from structured_query import import_tasks

    ids = [uuid4(), uuid4(), uuid4()]
    monkeypatch.setattr(api, "session_factory", lambda: lambda: nullcontext(object()))
    monkeypatch.setattr(
        api,
        "persist_dataset",
        lambda *args, **kwargs: SimpleNamespace(
            dataset=SimpleNamespace(id=ids[0]),
            version=SimpleNamespace(id=ids[1]),
            job=SimpleNamespace(id=ids[2]),
            created=True,
        ),
    )
    monkeypatch.setattr(api, "upload_stream", lambda *args, **kwargs: None)
    monkeypatch.setattr(import_tasks.import_dataset, "delay", lambda *args, **kwargs: None)
    response = client.post(
        "/v1/datasets/files",
        headers={"X-API-Key": "valid-key", "Idempotency-Key": "abcdefgh"},
        data={"namespace": "kb"},
        files={"file": ("data.csv", BytesIO("姓名\n张三\n".encode()), "text/csv")},
    )
    assert response.status_code == 202
    assert response.json()["state"] == "queued"


def test_database_registration_uses_secret_reference_contract(client, monkeypatch):
    from structured_query import api, profile_tasks

    ids = [uuid4(), uuid4(), uuid4()]
    monkeypatch.setattr(api, "session_factory", lambda: lambda: nullcontext(object()))
    monkeypatch.setattr(
        api,
        "register_datasource",
        lambda *args: (
            SimpleNamespace(id=ids[0]),
            SimpleNamespace(id=ids[1]),
            SimpleNamespace(id=ids[2]),
            True,
        ),
    )
    queued = []
    monkeypatch.setattr(profile_tasks.profile_datasource, "delay", queued.append)
    payload = {
        "kind": "database",
        "namespace": "kb",
        "name": "hr",
        "dialect": "mysql",
        "secret_ref": "tenant-a/hr",
        "catalog": "hr",
        "tables": [{"schema": "hr", "table": "people"}],
    }
    response = client.post(
        "/v1/datasets",
        headers={"X-API-Key": "valid-key", "Idempotency-Key": "database-request"},
        json=payload,
    )
    assert response.status_code == 202
    assert queued == [str(ids[2])]
    rejected = client.post(
        "/v1/datasets",
        headers={"X-API-Key": "valid-key", "Idempotency-Key": "database-request"},
        json={**payload, "dsn": "mysql://forbidden"},
    )
    assert rejected.status_code == 422


def test_database_profile_refresh_creates_and_dispatches_version(client, monkeypatch):
    from structured_query import api, profile_tasks

    ids = [uuid4(), uuid4(), uuid4()]
    monkeypatch.setattr(api, "session_factory", lambda: lambda: nullcontext(object()))
    monkeypatch.setattr(
        api,
        "refresh_datasource_profile",
        lambda *args: (*tuple(SimpleNamespace(id=item, state="queued") for item in ids), True),
    )
    queued = []
    monkeypatch.setattr(profile_tasks.profile_datasource, "delay", queued.append)
    response = client.post(
        f"/v1/datasets/{ids[0]}/profile-refresh",
        headers={"X-API-Key": "valid-key", "Idempotency-Key": "refresh-request"},
    )
    assert response.status_code == 202
    assert response.json()["version_id"] == str(ids[1])
    assert queued == [str(ids[2])]


def test_database_registration_retries_failed_dispatch_idempotently(client, monkeypatch):
    from structured_query import api, profile_tasks

    ids = [uuid4(), uuid4(), uuid4()]
    monkeypatch.setattr(api, "session_factory", lambda: lambda: nullcontext(object()))
    monkeypatch.setattr(
        api,
        "register_datasource",
        lambda *args: (
            SimpleNamespace(id=ids[0]), SimpleNamespace(id=ids[1]),
            SimpleNamespace(id=ids[2], state="failed"), False,
        ),
    )
    queued = []
    monkeypatch.setattr(profile_tasks.profile_datasource, "delay", queued.append)
    response = client.post(
        "/v1/datasets",
        headers={"X-API-Key": "valid-key", "Idempotency-Key": "same-registration"},
        json={
            "kind": "database", "namespace": "kb", "name": "hr",
            "dialect": "postgresql", "secret_ref": "tenant-a/hr", "catalog": "hr",
            "tables": [{"schema": "public", "table": "people"}],
        },
    )
    assert response.status_code == 202
    assert queued == [str(ids[2])]
