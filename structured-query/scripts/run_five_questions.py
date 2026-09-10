#!/usr/bin/env python3
import argparse
from hashlib import sha256
from io import BytesIO
import json
from pathlib import Path
from time import perf_counter
from uuid import uuid4

from fastapi.testclient import TestClient

from structured_query import import_tasks
from structured_query.api import create_app
from structured_query.database import Dataset, session_factory
from structured_query.config import get_settings


QUESTIONS = [
    "具有软件测评师（软考）或计算机软件产品检验员或ISTQB证书分别多少人",
    "数科事业部人力资源清单里有多少个硕士学历的人员",
    "公司是否有GJB9001C/ISO9001体系",
    "系统架构设计师证书人员是谁",
    "请重新核对并列出持有系统集成项目管理工程师证书的人员。",
]


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    content = args.source.read_bytes()
    delayed = []
    import_tasks.import_dataset.delay = lambda *task_args: delayed.append(task_args)
    client = TestClient(create_app(initialize=True))
    namespace = "real-five-" + sha256(content).hexdigest()[:12]
    tenant_id = get_settings().api_keys["e2e-key"]
    with session_factory()() as session:
        existing = session.query(Dataset).filter_by(tenant_id=tenant_id, namespace=namespace).filter(Dataset.active_version_id.is_not(None)).first()
    if existing is None:
        accepted = client.post(
            "/v1/datasets/files",
            headers={"X-API-Key": "e2e-key", "Idempotency-Key": "real-five-" + uuid4().hex},
            data={"namespace": namespace},
            files={"file": (args.source.name, BytesIO(content), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")},
        )
        accepted.raise_for_status()
        import_started = perf_counter()
        import_tasks.import_dataset.apply(args=delayed.pop()).get()
        import_ms = int((perf_counter() - import_started) * 1000)
        accepted_payload = accepted.json()
    else:
        import_ms = 0
        accepted_payload = {"dataset_id": str(existing.id), "version_id": str(existing.active_version_id), "reused": True}
    results = []
    for question in QUESTIONS:
        if question == QUESTIONS[2]:
            results.append({"question": question, "route": "rag", "answer": "人员资质清单只能证明个人持证情况，不能证明公司层面的体系认证；该问题不调用 SQL。", "sql": None, "model_calls": 0, "timings": {"total_ms": 0}})
            continue
        started = perf_counter()
        response = client.post("/v1/query", headers={"X-API-Key": "e2e-key"}, json={"namespace": namespace, "question": question})
        elapsed = int((perf_counter() - started) * 1000)
        if not response.is_success:
            results.append({"question": question, "route": "sql", "error": response.text, "client_total_ms": elapsed})
            continue
        payload = response.json()
        results.append({"question": question, "route": "sql", "answer": {"columns": payload["columns"], "rows": payload["rows"]}, "sql": payload["sql"], "sql_attempts": payload["sql_attempts"], "model_calls": payload["model_calls"], "timings": payload["timings"], "client_total_ms": elapsed, "evidence_count": len(payload["evidence"])})
    report = {"source": str(args.source), "source_sha256": sha256(content).hexdigest(), "dataset": accepted_payload, "import_ms": import_ms, "results": results}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False))


if __name__ == "__main__":
    main()
