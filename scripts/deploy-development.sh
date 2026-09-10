#!/usr/bin/env bash
set -euo pipefail

readonly repo_dir="/srv/weknora/current"
readonly shared_dir="/srv/weknora/shared"
readonly deployed_revision_file="$shared_dir/deployed-development-revision"

exec 9>"$shared_dir/deploy.lock"
flock -n 9 || {
  echo "Another WeKnora deployment is already running"
  exit 0
}

cd "$repo_dir"

git fetch --quiet origin development
remote_revision="$(git rev-parse origin/development)"
deployed_revision="$(cat "$deployed_revision_file" 2>/dev/null || true)"

if [[ "$remote_revision" == "$deployed_revision" ]]; then
  echo "WeKnora development is already deployed at $remote_revision"
  exit 0
fi

app_changed=false
frontend_changed=false
structured_query_changed=false
while IFS= read -r changed_file; do
  case "$changed_file" in
    structured-query/*)
      structured_query_changed=true
      ;;
    frontend/*)
      frontend_changed=true
      ;;
    docker-compose*.yml)
      app_changed=true
      frontend_changed=true
      structured_query_changed=true
      ;;
    *)
      app_changed=true
      ;;
  esac
done < <(git diff --name-only "$deployed_revision" "$remote_revision")

if ! docker inspect WeKnora-structured-query-api >/dev/null 2>&1 \
  || ! docker inspect WeKnora-structured-query-worker >/dev/null 2>&1; then
  structured_query_changed=true
fi

git checkout --quiet -B development origin/development
git reset --quiet --hard "$remote_revision"

export WEKNORA_VERSION="development-${remote_revision:0:12}-jenkins"
structured_query_key="$(sed -n 's/^WEKNORA_STRUCTURED_QUERY_API_KEY=//p' "$shared_dir/.env" | tail -n 1)"
if [[ -z "$structured_query_key" ]]; then
  structured_query_key="$(openssl rand -hex 32)"
  printf '\nWEKNORA_STRUCTURED_QUERY_API_KEY=%s\n' "$structured_query_key" >> "$shared_dir/.env"
fi
export WEKNORA_STRUCTURED_QUERY_API_KEY="$structured_query_key"
export WEKNORA_STRUCTURED_QUERY_ENABLED=true
if ! $structured_query_changed; then
  docker tag "$(docker inspect --format '{{.Image}}' WeKnora-structured-query-api)" \
    "weknora-structured-query:$WEKNORA_VERSION"
fi
compose=(
  docker compose
  --project-name weknora-standalone
  --env-file "$shared_dir/.env"
  -f "$repo_dir/docker-compose.yml"
  -f "$shared_dir/docker-compose.override.yml"
)

"${compose[@]}" config --quiet

build_services=()
if $app_changed; then
  build_services+=(app)
  app_rollback="$(docker inspect --format '{{.Image}}' WeKnora-app)"
  docker tag "$app_rollback" weknora-ci/app:rollback
fi
if $frontend_changed; then
  build_services+=(frontend)
  frontend_rollback="$(docker inspect --format '{{.Image}}' WeKnora-frontend)"
  docker tag "$frontend_rollback" weknora-ci/frontend:rollback
fi
if $structured_query_changed; then
  build_services+=(structured-query-api)
  if structured_query_rollback="$(docker inspect --format '{{.Image}}' WeKnora-structured-query-api 2>/dev/null)"; then
    docker tag "$structured_query_rollback" weknora-ci/structured-query:rollback
  else
    structured_query_rollback=""
  fi
fi

rollback() {
  echo "WeKnora deployment failed; restoring previous images" >&2
  if $app_changed; then
    docker tag weknora-ci/app:rollback "wechatopenai/weknora-app:$WEKNORA_VERSION"
    "${compose[@]}" up -d --no-deps --force-recreate app
    # The rollback container keeps running by image ID. Remove the failed
    # revision tag so the previous image cannot masquerade as a successful
    # build of the new commit.
    docker image rm "wechatopenai/weknora-app:$WEKNORA_VERSION" >/dev/null 2>&1 || true
  fi
  if $frontend_changed; then
    docker tag weknora-ci/frontend:rollback "wechatopenai/weknora-ui:$WEKNORA_VERSION"
    "${compose[@]}" up -d --no-deps --force-recreate frontend
    docker image rm "wechatopenai/weknora-ui:$WEKNORA_VERSION" >/dev/null 2>&1 || true
  fi
  if $structured_query_changed && [[ -n "$structured_query_rollback" ]]; then
    docker tag weknora-ci/structured-query:rollback "weknora-structured-query:$WEKNORA_VERSION"
    "${compose[@]}" up -d --no-deps --force-recreate structured-query-api structured-query-worker
  elif $structured_query_changed; then
    "${compose[@]}" stop structured-query-api structured-query-worker || true
  fi
}
trap rollback ERR

"${compose[@]}" build "${build_services[@]}"

# Always apply idempotent structured-query migrations before replacing the API
# or app. This prevents a healthy main app from silently degrading to RAG.
"${compose[@]}" run --rm --no-deps structured-query-migrate
"${compose[@]}" up -d --no-deps --force-recreate structured-query-api structured-query-worker

for attempt in {1..30}; do
  if [[ "$(docker inspect --format '{{.State.Health.Status}}' WeKnora-structured-query-api)" == "healthy" ]] \
    && [[ "$(docker inspect --format '{{.State.Health.Status}}' WeKnora-structured-query-worker)" == "healthy" ]]; then
    break
  fi
  if [[ "$attempt" == "30" ]]; then
    echo "WeKnora structured-query health check timed out" >&2
    exit 1
  fi
  sleep 2
done

if $app_changed; then
  "${compose[@]}" up -d --no-deps app

  for attempt in {1..60}; do
    if [[ "$(docker inspect --format '{{.State.Health.Status}}' WeKnora-app)" == "healthy" ]] \
      && curl --fail --silent --show-error http://127.0.0.1:18089/health >/dev/null; then
      break
    fi
    if [[ "$attempt" == "60" ]]; then
      echo "WeKnora app health check timed out" >&2
      exit 1
    fi
    sleep 5
  done
fi

# Submit historical completed spreadsheets that predate structured ingestion.
# The command is idempotent and skips documents already bound to a dataset.
backfill_started_at="$(date -u '+%Y-%m-%d %H:%M:%S+00')"
"${compose[@]}" run --rm --no-deps app \
  ./WeKnora-backfill-structured-query --all-tenants --concurrency 2

for attempt in {1..120}; do
  pending_jobs="$("${compose[@]}" exec -T postgres sh -ec \
    'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "$1"' _ \
    "select count(*) from sq_jobs where state in ('queued','processing')")"
  if [[ "$pending_jobs" == "0" ]]; then
    break
  fi
  if [[ "$attempt" == "120" ]]; then
    echo "Structured-query document ingestion timed out with $pending_jobs pending jobs" >&2
    exit 1
  fi
  sleep 5
done

failed_jobs="$("${compose[@]}" exec -T postgres sh -ec \
  'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "$1"' _ \
  "select count(*) from sq_jobs where state = 'failed' and created_at >= '$backfill_started_at'")"
if [[ "$failed_jobs" != "0" ]]; then
  echo "Structured-query document ingestion produced $failed_jobs failed jobs" >&2
  exit 1
fi

if $frontend_changed; then
  "${compose[@]}" up -d --no-deps frontend

  for attempt in {1..30}; do
    if curl --fail --silent --show-error http://127.0.0.1:8089/ >/dev/null; then
      break
    fi
    if [[ "$attempt" == "30" ]]; then
      echo "WeKnora frontend health check timed out" >&2
      exit 1
    fi
    sleep 2
  done
fi

trap - ERR
printf '%s\n' "$remote_revision" > "$deployed_revision_file"
echo "WeKnora development deployed successfully at $remote_revision"
