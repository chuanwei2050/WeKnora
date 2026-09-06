#!/usr/bin/env bash
set -euo pipefail

: "${ELASTICSEARCH_URL:=http://127.0.0.1:9200}"
: "${ELASTICSEARCH_USER:=elastic}"
: "${ELASTICSEARCH_PASSWORD:?ELASTICSEARCH_PASSWORD must be set}"
: "${SOURCE_INDEX:=weknora}"
: "${TARGET_INDEX:=weknora_ik}"

if [[ "${CONFIRM_REINDEX:-}" != "yes" ]]; then
  echo "Refusing to write Elasticsearch. Set CONFIRM_REINDEX=yes after checking SOURCE_INDEX and TARGET_INDEX." >&2
  exit 2
fi

es() {
  curl --fail-with-body --silent --show-error \
    --user "${ELASTICSEARCH_USER}:${ELASTICSEARCH_PASSWORD}" "$@"
}

if ! es "${ELASTICSEARCH_URL}/_nodes/plugins?filter_path=nodes.*.plugins.name" | grep -q 'analysis-ik'; then
  echo "analysis-ik is not installed on every Elasticsearch node." >&2
  exit 1
fi

es --head "${ELASTICSEARCH_URL}/${SOURCE_INDEX}" >/dev/null
if es --head "${ELASTICSEARCH_URL}/${TARGET_INDEX}" >/dev/null 2>&1; then
  echo "Target index ${TARGET_INDEX} already exists; refusing to overwrite it." >&2
  exit 1
fi

es -X PUT "${ELASTICSEARCH_URL}/${TARGET_INDEX}" \
  -H 'Content-Type: application/json' \
  --data-binary '{"mappings":{"properties":{"content":{"type":"text","analyzer":"ik_max_word","search_analyzer":"ik_smart"}}}}'

es -X POST "${ELASTICSEARCH_URL}/_reindex?wait_for_completion=true&refresh=true" \
  -H 'Content-Type: application/json' \
  --data-binary "{\"source\":{\"index\":\"${SOURCE_INDEX}\"},\"dest\":{\"index\":\"${TARGET_INDEX}\",\"op_type\":\"create\"}}"

source_count="$(es "${ELASTICSEARCH_URL}/${SOURCE_INDEX}/_count?filter_path=count")"
target_count="$(es "${ELASTICSEARCH_URL}/${TARGET_INDEX}/_count?filter_path=count")"
echo "Source ${SOURCE_INDEX}: ${source_count}"
echo "Target ${TARGET_INDEX}: ${target_count}"
echo "Migration finished. Verify retrieval, then set ELASTICSEARCH_INDEX=${TARGET_INDEX} and restart the application."
