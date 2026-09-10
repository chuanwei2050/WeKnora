from datetime import datetime
from enum import StrEnum
from typing import Any
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field


class JobState(StrEnum):
    QUEUED = "queued"
    RUNNING = "running"
    COMPLETED = "completed"
    FAILED = "failed"


class ErrorDetail(BaseModel):
    code: str
    message: str
    retryable: bool = False


class DatasetAccepted(BaseModel):
    dataset_id: UUID
    version_id: UUID
    job_id: UUID
    state: JobState = JobState.QUEUED


class AuthorizedTable(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)
    schema_name: str = Field(alias="schema", min_length=1, max_length=128)
    table: str = Field(min_length=1, max_length=128)


class FieldPolicy(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)
    schema_name: str = Field(alias="schema", min_length=1, max_length=128)
    table: str = Field(min_length=1, max_length=128)
    column: str = Field(min_length=1, max_length=128)
    persist_values: bool = False


class DatabaseDatasetCreate(BaseModel):
    model_config = ConfigDict(extra="forbid")
    kind: str = Field(pattern="^database$")
    namespace: str = Field(min_length=1, max_length=128)
    name: str = Field(min_length=1, max_length=128)
    dialect: str = Field(pattern="^(postgresql|mysql)$")
    secret_ref: str = Field(min_length=1, max_length=256)
    catalog: str = Field(min_length=1, max_length=128)
    tables: list[AuthorizedTable] = Field(min_length=1, max_length=256)
    field_policies: list[FieldPolicy] = Field(default_factory=list, max_length=2048)


class JobResponse(BaseModel):
    job_id: UUID
    state: JobState
    progress: int = Field(ge=0, le=100)
    created_at: datetime
    updated_at: datetime
    error: ErrorDetail | None = None


class QueryRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    namespace: str = Field(min_length=1, max_length=128)
    question: str = Field(min_length=1, max_length=4_000)
    dataset_ids: list[UUID] = Field(default_factory=list, max_length=2_048)


class Evidence(BaseModel):
    kind: str
    table_id: UUID
    column_id: UUID | None = None
    text: str
    score: float


class SourceRef(BaseModel):
    dataset_id: UUID
    version_id: UUID
    table_id: UUID
    original_file_name: str
    sheet_name: str


class QueryTimings(BaseModel):
    retrieval_ms: int = 0
    metadata_scope_ms: int = 0
    lexical_ms: int = 0
    embedding_ms: int = 0
    vector_ms: int = 0
    table_selection_ms: int = 0
    target_value_refine_ms: int = 0
    model_ms: int = 0
    validation_ms: int = 0
    execution_ms: int = 0
    total_ms: int = 0


class QueryResponse(BaseModel):
    route: str = "sql"
    sql: str = ""
    sql_attempts: list[str] = Field(default_factory=list)
    columns: list[str] = Field(default_factory=list)
    rows: list[list[Any]] = Field(default_factory=list)
    evidence: list[Evidence] = Field(default_factory=list)
    sources: list[SourceRef] = Field(default_factory=list)
    timings: QueryTimings = Field(default_factory=QueryTimings)
    model_calls: int = Field(ge=0, le=2)


class HealthResponse(BaseModel):
    status: str
    dependencies: dict[str, str] = Field(default_factory=dict)
