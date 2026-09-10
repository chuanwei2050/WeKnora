FROM python:3.12-slim AS builder

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /build
COPY pyproject.toml README.md ./
COPY src ./src
RUN python -m pip install --no-cache-dir --prefix=/install .

FROM python:3.12-slim
RUN groupadd --system app && useradd --system --gid app --create-home app
COPY --from=builder /install /usr/local
WORKDIR /app
COPY alembic.ini ./
COPY migrations ./migrations
USER app
EXPOSE 8080
ENTRYPOINT ["python", "-m", "structured_query"]
CMD ["api"]
