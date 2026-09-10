#!/usr/bin/env python3
import argparse
import json
from pathlib import Path

import httpx

from structured_query.evaluation import evaluate_response


def main() -> int:
    parser = argparse.ArgumentParser(description="Run execution-accuracy cases against structured-query")
    parser.add_argument("cases", type=Path, help="JSON array of question and expected result cases")
    parser.add_argument("--base-url", default="http://127.0.0.1:8090")
    parser.add_argument("--api-key", required=True)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--tenant-id", help="Required when the API key is configured as a service key")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()

    cases = json.loads(args.cases.read_text(encoding="utf-8"))
    if not isinstance(cases, list) or not cases:
        raise ValueError("cases_must_be_non_empty_array")
    report = []
    with httpx.Client(base_url=args.base_url, timeout=240) as client:
        for case in cases:
            headers = {"X-API-Key": args.api_key}
            if args.tenant_id:
                headers["X-Tenant-ID"] = args.tenant_id
            response = client.post(
                "/v1/query",
                headers=headers,
                json={"namespace": args.namespace, "question": case["question"]},
            )
            payload = response.json() if response.is_success else {"http_status": response.status_code}
            failures = (
                evaluate_response(case, payload)
                if response.is_success
                else [{"code": "http_error", "detail": str(response.status_code)}]
            )
            report.append({
                "question": case["question"],
                "passed": not failures,
                "failures": [failure.__dict__ if hasattr(failure, "__dict__") else failure for failure in failures],
                "response": payload,
            })
    rendered = json.dumps(report, ensure_ascii=False, indent=2)
    if args.output:
        args.output.write_text(rendered, encoding="utf-8")
    print(rendered)
    return 0 if all(item["passed"] for item in report) else 1


if __name__ == "__main__":
    raise SystemExit(main())
