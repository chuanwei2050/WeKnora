from functools import lru_cache
from typing import Annotated
from urllib.parse import quote

from pydantic import Field, SecretStr, field_validator, model_validator
from pydantic_settings import BaseSettings, NoDecode, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="STRUCTURED_QUERY_", case_sensitive=False, extra="ignore"
    )

    postgres_dsn: SecretStr | None = None
    postgres_host: str = "postgres"
    postgres_port: int = Field(default=5432, ge=1, le=65535)
    postgres_user: str = ""
    postgres_password: SecretStr = SecretStr("")
    postgres_database: str = ""
    redis_url: SecretStr | None = None
    redis_host: str = "redis"
    redis_port: int = Field(default=6379, ge=1, le=65535)
    redis_password: SecretStr = SecretStr("")
    redis_database: int = Field(default=2, ge=0, le=15)
    s3_endpoint: str
    s3_access_key: SecretStr
    s3_secret_key: SecretStr
    s3_bucket: str = "structured-query-staging"
    s3_region: str = "us-east-1"
    milvus_uri: str
    milvus_token: SecretStr = SecretStr("")
    milvus_database: str = "default"
    milvus_collection_prefix: str = "weknora_structured"

    weknora_base_url: str
    weknora_service_key: SecretStr
    model_config_ttl_seconds: int = Field(default=30, ge=0, le=3600)
    model_request_timeout_seconds: int = Field(default=180, ge=10, le=600)
    sql_max_completion_tokens: int = Field(default=512, ge=128, le=4096)
    embedding_batch_size: int = Field(default=64, ge=1, le=512)

    api_keys: Annotated[dict[str, str], NoDecode]
    datasource_secrets: dict[str, SecretStr] = Field(default_factory=dict)
    max_upload_bytes: int = Field(default=100 * 1024 * 1024, gt=0)
    max_tables: int = Field(default=3, ge=1, le=3)
    max_result_rows: int = Field(default=200, ge=1, le=10_000)
    max_result_bytes: int = Field(default=8 * 1024 * 1024, ge=1024, le=64 * 1024 * 1024)
    max_result_cell_bytes: int = Field(default=1024 * 1024, ge=1024, le=8 * 1024 * 1024)
    statement_timeout_ms: int = Field(default=10_000, ge=100, le=120_000)
    lock_timeout_ms: int = Field(default=1_000, ge=10, le=30_000)
    max_plan_cost: float = Field(default=1_000_000, gt=0)
    max_plan_rows: int = Field(default=10_000_000, gt=0)
    low_cardinality_limit: int = Field(default=2_000, ge=10, le=100_000)
    high_cardinality_sample: int = Field(default=500, ge=10, le=10_000)
    import_lock_seconds: int = Field(default=900, ge=30, le=86_400)
    max_import_sheets: int = Field(default=64, ge=1, le=1_024)
    max_import_rows_per_sheet: int = Field(default=1_000_000, ge=1_000, le=10_000_000)
    max_import_columns: int = Field(default=512, ge=1, le=10_000)
    datasource_profile_concurrency: int = Field(default=2, ge=1, le=8)
    external_profile_max_age_seconds: int = Field(default=86_400, ge=60, le=31_536_000)
    max_profile_documents_per_version: int = Field(default=200_000, ge=1_000, le=5_000_000)
    max_excel_uncompressed_bytes: int = Field(default=512 * 1024 * 1024, ge=1024 * 1024)
    max_excel_archive_entries: int = Field(default=10_000, ge=100, le=1_000_000)

    @model_validator(mode="after")
    def build_connection_urls(self) -> "Settings":
        if self.postgres_dsn is None:
            if not self.postgres_user or not self.postgres_database:
                raise ValueError("postgres_dsn or postgres component settings are required")
            user = quote(self.postgres_user, safe="")
            password = quote(self.postgres_password.get_secret_value(), safe="")
            database = quote(self.postgres_database, safe="")
            self.postgres_dsn = SecretStr(
                f"postgresql+psycopg://{user}:{password}@{self.postgres_host}:{self.postgres_port}/{database}"
            )
        if self.redis_url is None:
            password = quote(self.redis_password.get_secret_value(), safe="")
            auth = f":{password}@" if password else ""
            self.redis_url = SecretStr(
                f"redis://{auth}{self.redis_host}:{self.redis_port}/{self.redis_database}"
            )
        return self

    @field_validator("api_keys", mode="before")
    @classmethod
    def parse_api_keys(cls, value: object) -> object:
        if isinstance(value, str):
            pairs: dict[str, str] = {}
            for entry in value.split(","):
                key, separator, tenant = entry.strip().partition(":")
                if not separator or not key or not tenant:
                    raise ValueError("api_keys must use key:tenant comma-separated entries")
                if key.startswith("CHANGE_ME_"):
                    raise ValueError("replace placeholder API keys before starting the service")
                pairs[key] = tenant
            return pairs
        return value


@lru_cache
def get_settings() -> Settings:
    return Settings()  # type: ignore[call-arg]
