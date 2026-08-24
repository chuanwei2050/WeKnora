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

git checkout --quiet -B development origin/development
git reset --quiet --hard "$remote_revision"

export WEKNORA_VERSION="development-${remote_revision:0:12}-jenkins"
compose=(
  docker compose
  --project-name weknora-standalone
  --env-file "$shared_dir/.env"
  -f "$repo_dir/docker-compose.yml"
  -f "$shared_dir/docker-compose.override.yml"
)

"${compose[@]}" config --quiet

app_rollback="$(docker inspect --format '{{.Image}}' WeKnora-app)"
frontend_rollback="$(docker inspect --format '{{.Image}}' WeKnora-frontend)"
docker tag "$app_rollback" weknora-ci/app:rollback
docker tag "$frontend_rollback" weknora-ci/frontend:rollback

rollback() {
  echo "WeKnora deployment failed; restoring previous app images" >&2
  docker tag weknora-ci/app:rollback "wechatopenai/weknora-app:$WEKNORA_VERSION"
  docker tag weknora-ci/frontend:rollback "wechatopenai/weknora-ui:$WEKNORA_VERSION"
  "${compose[@]}" up -d --no-deps --force-recreate app
  "${compose[@]}" up -d --no-deps --force-recreate frontend
}
trap rollback ERR

"${compose[@]}" build app frontend
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

trap - ERR
printf '%s\n' "$remote_revision" > "$deployed_revision_file"
echo "WeKnora development deployed successfully at $remote_revision"
