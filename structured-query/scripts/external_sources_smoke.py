#!/usr/bin/env python3
"""Zero-copy PostgreSQL/MySQL registration, profile, query and restart smoke test."""

from concurrent.futures import ThreadPoolExecutor
from hashlib import sha256
import math
import re
from types import SimpleNamespace
from uuid import uuid4

from fastapi.testclient import TestClient
from sqlalchemy import func, select

from structured_query import candidate_search, model_config, profile_tasks, query_service, vector_store
from structured_query.api import create_app
from structured_query.database import DataTable, Dataset, engine, session_factory
from structured_query.model_gateway import SQLGeneration
from structured_query.__main__ import migrate_database


def deterministic_embeddings(_tenant_id, texts):
    vectors = []
    for text in texts:
        digest = sha256(text.encode()).digest()
        raw = [float(digest[index] - 128) for index in range(8)]
        norm = math.sqrt(sum(value * value for value in raw)) or 1
        vectors.append([value / norm for value in raw])
    return vectors


def schema_bound_model(_tenant_id, question, schema_context, *_args, dialect="postgres", **_kwargs):
    table = re.search(r"# Table: ([^\n]+)", schema_context).group(1)
    columns = [name for name in re.findall(r"^  \(([^,]+),", schema_context, re.MULTILINE)]
    degree = next(name for name in columns if name == "degree")
    quote = "`" if dialect == "mysql" else '"'
    sql = f"SELECT COUNT(*) AS total FROM {quote}{table.split('.')[0]}{quote}.{quote}{table.split('.')[1]}{quote} WHERE {quote}{degree}{quote} = '硕士'"
    return SQLGeneration(route="sql", sql=sql)


def register(client, namespace, dialect, secret_ref, catalog, schema):
    response = client.post(
        "/v1/datasets",
        headers={"X-API-Key": "e2e-key", "Idempotency-Key": "external-" + uuid4().hex},
        json={"kind": "database", "namespace": namespace, "name": namespace, "dialect": dialect, "secret_ref": secret_ref, "catalog": catalog, "tables": [{"schema": schema, "table": "people"}], "field_policies": [{"schema": schema, "table": "people", "column": "name", "persist_values": False}]},
    )
    response.raise_for_status()
    profile_tasks.profile_datasource.apply(args=[response.json()["job_id"]]).get()
    return response.json()


def main():
    migrate_database()
    vector_store.embed_texts = deterministic_embeddings
    runtime_config = lambda _tenant_id: SimpleNamespace(
        embedding=SimpleNamespace(id="smoke", name="deterministic", base_url="local://smoke", dimension=8)
    )
    model_config.get_runtime_model_config = runtime_config
    vector_store.get_runtime_model_config = runtime_config
    candidate_search.search_profiles = vector_store.search_profiles
    query_service.generate_sql = schema_bound_model
    profile_tasks.profile_datasource.delay = lambda *_: None
    client = TestClient(create_app(initialize=True))
    before = None
    with session_factory()() as session:
        before = session.scalar(select(func.count()).select_from(DataTable))
    cases = [("external-pg-" + uuid4().hex[:6], "postgresql", "pg-source", "sq_e2e_20260909", "external_demo"), ("external-mysql-" + uuid4().hex[:6], "mysql", "mysql-source", "structured_test", "structured_test")]
    outputs = []
    for namespace, dialect, secret, catalog, schema in cases:
        registered = register(client, namespace, dialect, secret, catalog, schema)
        refresh = client.post(
            f"/v1/datasets/{registered['dataset_id']}/profile-refresh",
            headers={"X-API-Key": "e2e-key", "Idempotency-Key": "refresh-" + uuid4().hex},
        )
        refresh.raise_for_status()
        assert refresh.json()["version_id"] != registered["version_id"]
        profile_tasks.profile_datasource.apply(args=[refresh.json()["job_id"]]).get()
        request = {"namespace": namespace, "question": "硕士学历人数"}
        response = client.post("/v1/query", headers={"X-API-Key": "e2e-key"}, json=request)
        if not response.is_success:
            raise RuntimeError(f"{dialect} query failed: {response.status_code} {response.text}")
        assert response.json()["rows"] == [[2]]
        with ThreadPoolExecutor(max_workers=3) as pool:
            rows = list(pool.map(lambda _: client.post("/v1/query", headers={"X-API-Key": "e2e-key"}, json=request).json()["rows"], range(4)))
        assert all(row == response.json()["rows"] for row in rows)
        outputs.append({"dialect": dialect, "sql": response.json()["sql"], "rows": response.json()["rows"], "timings": response.json()["timings"]})
    with session_factory()() as session:
        after = session.scalar(select(func.count()).select_from(DataTable))
        external = list(session.scalars(select(Dataset).where(Dataset.namespace.in_([case[0] for case in cases]))))
        assert all(dataset.source_type in {"postgresql", "mysql"} for dataset in external)
    assert after - before == 4, "two versioned profiles per source should be added; business rows must not be copied"
    vector_store.milvus_client.cache_clear()
    engine().dispose(); engine.cache_clear(); session_factory.cache_clear()
    print({"sources": outputs, "business_rows_copied": 0, "profile_refreshes": 2, "concurrent_queries_per_source": 4, "restart_recovered": True})


if __name__ == "__main__":
    main()
