#!/usr/bin/env python3
"""Real PostgreSQL/Redis/MinIO/Milvus smoke test with deterministic model doubles."""

from concurrent.futures import ThreadPoolExecutor
from hashlib import sha256
from io import BytesIO
import math
import re
from types import SimpleNamespace
from uuid import uuid4

import pandas as pd
from fastapi.testclient import TestClient

from structured_query import candidate_search, import_tasks, model_config, query_service, vector_store
from structured_query.api import create_app
from structured_query.database import Dataset, engine, session_factory
from structured_query.model_gateway import SQLGeneration
from structured_query.__main__ import migrate_database


def deterministic_embeddings(_tenant_id: str, texts: list[str]) -> list[list[float]]:
    dimension = 8
    result = []
    for text in texts:
        digest = sha256(text.encode()).digest()
        vector = [float(digest[index] - 128) for index in range(dimension)]
        norm = math.sqrt(sum(value * value for value in vector)) or 1
        result.append([value / norm for value in vector])
    return result


def workbook() -> bytes:
    stream = BytesIO()
    with pd.ExcelWriter(stream, engine="openpyxl") as writer:
        pd.DataFrame({"姓名": ["甲", "乙", "丙"], "学历": ["硕士", "本科", "硕士"]}).to_excel(writer, sheet_name="人员", index=False)
        pd.DataFrame({"项目": ["A", "B"], "状态": ["完成", "进行中"]}).to_excel(writer, sheet_name="项目", index=False)
    return stream.getvalue()


def schema_bound_model(_tenant_id: str, question: str, schema_context: str, *_args, **_kwargs) -> SQLGeneration:
    table = re.search(r"# Table: ([^,\n]+)", schema_context).group(1)
    columns = dict((original, physical) for physical, original in re.findall(r"\((c_\d+):([^,]+),", schema_context))
    if "硕士" in question and "学历" in columns:
        sql = f'SELECT COUNT(*) AS total FROM "{table}" WHERE "{columns["学历"]}" = \'硕士\''
    else:
        sql = f'SELECT COUNT(*) AS total FROM "{table}"'
    return SQLGeneration(route="sql", sql=sql)


def main() -> None:
    migrate_database()
    namespace = "full-stack-" + uuid4().hex[:8]
    vector_store.embed_texts = deterministic_embeddings
    runtime_config = lambda _tenant_id: SimpleNamespace(
        embedding=SimpleNamespace(id="smoke", name="deterministic", base_url="local://smoke", dimension=8)
    )
    model_config.get_runtime_model_config = runtime_config
    vector_store.get_runtime_model_config = runtime_config
    query_service.generate_sql = schema_bound_model
    candidate_search.search_profiles = vector_store.search_profiles
    delayed: list[tuple] = []
    import_tasks.import_dataset.delay = lambda *args: delayed.append(args)
    client = TestClient(create_app(initialize=True))
    content = workbook()
    response = client.post(
        "/v1/datasets/files",
        headers={"X-API-Key": "e2e-key", "Idempotency-Key": "full-stack-" + uuid4().hex},
        data={"namespace": namespace},
        files={"file": ("smoke.xlsx", content, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")},
    )
    response.raise_for_status()
    import_tasks.import_dataset.apply(args=delayed.pop()).get()
    answer = client.post("/v1/query", headers={"X-API-Key": "e2e-key"}, json={"namespace": namespace, "question": "硕士人数"})
    if not answer.is_success:
        raise RuntimeError(f"query failed: {answer.status_code} {answer.text}")
    payload = answer.json()
    assert payload["rows"] == [[2]] and payload["model_calls"] == 1
    with ThreadPoolExecutor(max_workers=4) as pool:
        concurrent = list(pool.map(lambda _: client.post("/v1/query", headers={"X-API-Key": "e2e-key"}, json={"namespace": namespace, "question": "硕士人数"}).json()["rows"], range(8)))
    assert concurrent == [[[2]]] * 8
    isolated = client.post("/v1/query", headers={"X-API-Key": "other-key"}, json={"namespace": namespace, "question": "硕士人数"})
    assert isolated.status_code == 422
    vector_store.milvus_client.cache_clear()
    engine().dispose()
    engine.cache_clear()
    session_factory.cache_clear()
    restarted = client.post("/v1/query", headers={"X-API-Key": "e2e-key"}, json={"namespace": namespace, "question": "硕士人数"})
    restarted.raise_for_status()
    assert restarted.json()["rows"] == [[2]]
    with session_factory()() as session:
        dataset = session.query(Dataset).filter_by(namespace=namespace).one()
        print({"answer": payload["rows"], "sql": payload["sql"], "timings": payload["timings"], "model_calls": payload["model_calls"], "dataset_id": str(dataset.id), "concurrent_queries": len(concurrent), "restart_recovered": True})


if __name__ == "__main__":
    main()
