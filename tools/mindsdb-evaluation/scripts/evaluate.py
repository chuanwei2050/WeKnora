from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import time
import urllib.request
from pathlib import Path
from typing import Any

import pandas as pd
from sqlalchemy import create_engine, text

SCHEMA = "mindsdb_evaluation"
ROLE = "mindsdb_evaluation_reader"
SOURCE = "evaluation_pg"
AGENT = "evaluation_agent"
TABLE = "personnel_qualifications"
QUESTIONS = [
    "具有软件测评师（软考）或计算机软件产品检验员或ISTQB证书分别多少人",
    "数科事业部人力资源清单里有多少个硕士学历的人员",
    "公司是否有GJB9001C/ISO9001体系",
    "系统架构设计师证书人员是谁",
    "请重新核对并列出持有系统集成项目管理工程师证书的人员。",
]


def required(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        raise ValueError(f"missing environment variable: {name}")
    return value


def sql_api(query: str) -> dict[str, Any]:
    request = urllib.request.Request(
        required("MINDSDB_HTTP_URL").rstrip("/") + "/api/sql/query",
        data=json.dumps({"query": query}).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=300) as response:
        result = json.load(response)
    if result.get("type") == "error":
        raise RuntimeError(result.get("error_message") or result)
    return result


def prepare() -> None:
    path = Path(required("EVAL_SOURCE_FILE"))
    frames = pd.read_excel(path, sheet_name=None)
    frame = next(frame for frame in frames.values() if not frame.dropna(how="all").empty)
    frame = frame.dropna(how="all").dropna(axis=1, how="all")
    admin = create_engine(required("EVAL_POSTGRES_ADMIN_URL"))
    password = required("EVAL_POSTGRES_READ_PASSWORD")
    revision = hashlib.sha256(path.read_bytes()).hexdigest()
    with admin.begin() as connection:
        connection.execute(text(f'DROP SCHEMA IF EXISTS "{SCHEMA}" CASCADE'))
        connection.execute(text(f'CREATE SCHEMA "{SCHEMA}"'))
    frame.to_sql(TABLE, admin, schema=SCHEMA, if_exists="replace", index=False)
    with admin.begin() as connection:
        raw = connection.connection.driver_connection
        with raw.cursor() as cursor:
            cursor.execute("SELECT 1 FROM pg_roles WHERE rolname=%s", (ROLE,))
            action = "ALTER ROLE" if cursor.fetchone() else "CREATE ROLE"
            cursor.execute(f'{action} "{ROLE}" LOGIN PASSWORD %s', (password,))
        connection.execute(text(f'GRANT USAGE ON SCHEMA "{SCHEMA}" TO "{ROLE}"'))
        connection.execute(text(f'GRANT SELECT ON ALL TABLES IN SCHEMA "{SCHEMA}" TO "{ROLE}"'))
        connection.execute(text(f'COMMENT ON TABLE "{SCHEMA}"."{TABLE}" IS '
                                "'知识库上传的人力资源清单，包含人员基本信息、学历、部门、职称和证书。文件或清单名称描述数据范围，不是行过滤值。'"))
    print(json.dumps({"revision": revision, "rows": len(frame), "columns": len(frame.columns)}))


def setup() -> None:
    for query in (f"DROP AGENT IF EXISTS {AGENT}", f"DROP DATABASE IF EXISTS {SOURCE}"):
        try:
            sql_api(query)
        except RuntimeError as exc:
            if "does not exist" not in str(exc):
                raise
    connection = {
        "host": required("EVAL_POSTGRES_HOST"),
        "port": int(required("EVAL_POSTGRES_PORT")),
        "database": required("EVAL_POSTGRES_DATABASE"),
        "user": ROLE,
        "password": required("EVAL_POSTGRES_READ_PASSWORD"),
        "schema": SCHEMA,
    }
    sql_api(f"CREATE DATABASE {SOURCE} WITH ENGINE = 'postgres', PARAMETERS = {json.dumps(connection)}")
    count = sql_api(f"SELECT COUNT(*) AS row_count FROM {SOURCE}.{TABLE}")
    model = {
        "provider": "openai",
        "model_name": required("LLM_MODEL_NAME"),
        "api_key": required("OPENAI_API_KEY"),
        "base_url": required("OPENAI_API_BASE"),
    }
    data = {"tables": [f"{SOURCE}.{TABLE}"]}
    prompt = (
        "This table is an uploaded personnel and qualification spreadsheet. "
        "Use actual database values and all semantically relevant columns. "
        "A workbook, list, organization, or dataset name describes scope and must not become a row filter unless it is an actual stored value. "
        "Before querying, verify that the entity level requested by the question exists in the table; employee credentials do not prove organization-level systems or certifications. "
        "If the table cannot answer the requested scope, say so instead of returning zero. "
        "For every non-ASCII SQL identifier use identifier quoting, and use ASCII-only result aliases because the proxy SQL parser rejects unquoted non-ASCII aliases."
    )
    sql_api(f"CREATE AGENT {AGENT} USING model = {json.dumps(model)}, data = {json.dumps(data)}, "
            f"prompt_template = {json.dumps(prompt)}, timeout = 240, mode = 'sql'")
    print(json.dumps({"source": SOURCE, "agent": AGENT, "count": count}, ensure_ascii=False))


def benchmark(output: Path) -> None:
    results = []
    for question in QUESTIONS:
        started = time.perf_counter()
        escaped = question.replace("'", "''")
        try:
            response = sql_api(f"SELECT * FROM {AGENT} WHERE question = '{escaped}'")
            error = None
        except Exception as exc:
            response = None
            error = f"{type(exc).__name__}: {exc}"
        results.append({"question": question, "response": response, "error": error,
                        "total_ms": round((time.perf_counter() - started) * 1000)})
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({"results": str(output), "questions": len(results)}, ensure_ascii=False))


def cleanup() -> None:
    for query in (f"DROP AGENT IF EXISTS {AGENT}", f"DROP DATABASE IF EXISTS {SOURCE}"):
        try:
            sql_api(query)
        except Exception:
            pass
    admin = create_engine(required("EVAL_POSTGRES_ADMIN_URL"))
    with admin.begin() as connection:
        connection.execute(text(f'DROP SCHEMA IF EXISTS "{SCHEMA}" CASCADE'))
        connection.execute(text(f'DROP ROLE IF EXISTS "{ROLE}"'))
    print(json.dumps({"cleaned": True}))


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=["prepare", "setup", "benchmark", "cleanup"])
    parser.add_argument("--output", type=Path, default=Path("results.json"))
    args = parser.parse_args()
    if args.command == "benchmark":
        benchmark(args.output)
    else:
        {"prepare": prepare, "setup": setup, "cleanup": cleanup}[args.command]()


if __name__ == "__main__":
    main()
