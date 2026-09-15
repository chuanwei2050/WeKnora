# uv/uvx is copied from an explicit, cacheable image instead of being installed
# through a network shell script. Air-gapped builders must preload this image or
# override UV_IMAGE with an approved internal mirror digest.
ARG UV_IMAGE=ghcr.io/astral-sh/uv@sha256:75bc2f1d328b6d5bf38bf7120dcfebf619b932bd78570c8ea1ae93db25b25ace
ARG APP_RUNTIME_IMAGE=weknora-ci/app-runtime:bookworm-v1
FROM ${UV_IMAGE} AS uv

# Build stage
FROM golang:1.24-bookworm AS builder

WORKDIR /app

# 通过构建参数接收敏感信息
ARG GOPRIVATE_ARG
ARG GOPROXY_ARG
ARG GOSUMDB_ARG=off
ARG APK_MIRROR_ARG
ARG APT_MIRROR_ARG

# 设置Go环境变量
ENV GOPRIVATE=${GOPRIVATE_ARG}
ENV GOPROXY=${GOPROXY_ARG}
ENV GOSUMDB=${GOSUMDB_ARG}

# Install dependencies
RUN if [ -n "$APK_MIRROR_ARG" ]; then \
        sed -i -E "s@https?://(deb|security).debian.org@http://${APK_MIRROR_ARG}@g" /etc/apt/sources.list.d/debian.sources; \
    fi && \
    if [ -n "$APT_MIRROR_ARG" ]; then \
        sed -i -E "s@https?://(deb|security).debian.org@${APT_MIRROR_ARG}@g" /etc/apt/sources.list.d/debian.sources; \
    fi && \
    apt-get update && \
    apt-get install -y git build-essential libsqlite3-dev

# Keep dependency and compiler caches stable across Dockerfile layer misses.
# Downloads are retried because public proxies can close large responses early.
RUN --mount=type=cache,id=weknora-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=weknora-go-build,target=/root/.cache/go-build,sharing=locked \
    for attempt in 1 2 3; do \
        go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.1 && break; \
        if [ "$attempt" -eq 3 ]; then exit 1; fi; \
        sleep $((attempt * 3)); \
    done

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN --mount=type=cache,id=weknora-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=weknora-go-build,target=/root/.cache/go-build,sharing=locked \
    for attempt in 1 2 3; do \
        go mod download && break; \
        if [ "$attempt" -eq 3 ]; then exit 1; fi; \
        sleep $((attempt * 3)); \
    done
COPY cmd/download cmd/download
RUN go run cmd/download/duckdb/duckdb.go
COPY . .

# Get version and commit info for build injection
ARG VERSION_ARG
ARG COMMIT_ID_ARG
ARG BUILD_TIME_ARG
ARG GO_VERSION_ARG

# Set build-time variables
ENV VERSION=${VERSION_ARG}
ENV COMMIT_ID=${COMMIT_ID_ARG}
ENV BUILD_TIME=${BUILD_TIME_ARG}
ENV GO_VERSION=${GO_VERSION_ARG}

# Build the application with version info
RUN --mount=type=cache,id=weknora-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=weknora-go-build,target=/root/.cache/go-build,sharing=locked \
    make build-prod
RUN --mount=type=cache,id=weknora-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=weknora-go-build,target=/root/.cache/go-build,sharing=locked \
    go build -o /app/WeKnora-backfill-structured-query ./cmd/backfill-structured-query
RUN --mount=type=cache,id=weknora-go-mod,target=/go/pkg/mod,sharing=locked \
    cp -r /go/pkg/mod/github.com/yanyiwu/ /app/yanyiwu/

# Final stage: fixed OS dependencies live in a separately versioned image.
FROM ${APP_RUNTIME_IMAGE}

WORKDIR /app

# Keep MCP stdio support available without any runtime download. The source
# image is explicit so it can be mirrored and verified as part of the offline
# image set.
COPY --from=uv /usr/local/bin/uv /usr/local/bin/uv
COPY --from=uv /usr/local/bin/uvx /usr/local/bin/uvx

# Copy migrate tool from builder stage
COPY --from=builder /go/bin/migrate /usr/local/bin/
COPY --from=builder /app/yanyiwu/ /go/pkg/mod/github.com/yanyiwu/

# Copy the binary from the builder stage
COPY --from=builder /app/config ./config
COPY --from=builder /app/scripts ./scripts
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/dataset/samples ./dataset/samples
COPY --from=builder /app/skills/preloaded ./skills/preloaded
# Keep a read-only backup so bind-mount cannot erase built-in skills
COPY --from=builder /app/skills/preloaded ./skills/_builtin
COPY --from=builder /root/.duckdb /home/appuser/.duckdb
COPY --from=builder /app/WeKnora .
COPY --from=builder /app/WeKnora-backfill-structured-query .

# Copy and make entrypoint script executable
COPY --from=builder /app/scripts/docker-entrypoint.sh ./scripts/docker-entrypoint.sh

# Make scripts executable
RUN chmod +x ./scripts/*.sh

# Expose ports
EXPOSE 8080


ENTRYPOINT ["./scripts/docker-entrypoint.sh"]
CMD ["./WeKnora"]
