#!/bin/bash
set -euo pipefail

API="${API:-http://127.0.0.1:18080}"
ENV_FILE="${ENV_FILE:-/data/weknora-standalone/.env}"
PGPASSWORD="${PGPASSWORD:-1f58457e80df3a8cae944dcc7da1898e53ae1519400d760d}"

TOKEN=$(curl -s -X POST "$API/api/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@weknora.local","password":"Admin@123456"}' \
  | python3 -c 'import sys,json; d=json.load(sys.stdin); print((d.get("data") or {}).get("token") or d.get("token") or "")')

if [ -z "$TOKEN" ]; then
  echo "login failed" >&2
  exit 1
fi

AUTH="Authorization: Bearer $TOKEN"
OFFLINE_KEY=$(grep '^OFFLINE_LLM_MODEL_API_KEY=' "$ENV_FILE" | cut -d= -f2-)

update_model() {
  local id="$1"
  local name="$2"
  local api_key="$3"
  local payload
  if [ -n "$api_key" ]; then
    payload=$(python3 - <<PY
import json
print(json.dumps({"name": "$name", "parameters": {"api_key": "$api_key"}}))
PY
)
  else
    payload=$(python3 - <<PY
import json
print(json.dumps({"name": "$name"}))
PY
)
  fi
  curl -s -X PUT "$API/api/v1/models/$id" \
    -H 'Content-Type: application/json' \
    -H "$AUTH" \
    -d "$payload"
  echo
}

psql_query() {
  docker exec -e PGPASSWORD="$PGPASSWORD" WeKnora-postgres \
    psql -U weknora -d weknora -At -F'|' -c "$1"
}

while IFS='|' read -r id role; do
  [ -z "$id" ] && continue
  echo "Updating offline $role ($id)..."
  update_model "$id" "Qwen3.5-122B-A10B-FP8" "$OFFLINE_KEY"
done < <(psql_query "SELECT id, profile_role FROM models WHERE tenant_id=0 AND profile='offline' AND profile_role IN ('chat','verifier_2','evaluation_judge','vlm');")

while IFS='|' read -r id role; do
  [ -z "$id" ] && continue
  echo "Updating online $role ($id)..."
  update_model "$id" "deepseek-ai/DeepSeek-V4-Pro" ""
done < <(psql_query "SELECT id, profile_role FROM models WHERE tenant_id=0 AND profile='online' AND profile_role IN ('chat','evaluation_judge','vlm');")

echo '--- verify ---'
docker exec -e PGPASSWORD="$PGPASSWORD" WeKnora-postgres \
  psql -U weknora -d weknora -c \
  "SELECT name, profile, profile_role FROM models WHERE tenant_id=0 AND profile_role IN ('chat','evaluation_judge','verifier_2','vlm') ORDER BY profile, profile_role;"
