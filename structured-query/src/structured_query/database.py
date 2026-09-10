from collections.abc import Generator
from datetime import datetime
from enum import StrEnum
from functools import lru_cache
from uuid import UUID, uuid4

from sqlalchemy import JSON, Boolean, DateTime, ForeignKey, Index, Integer, String, Text, UniqueConstraint, create_engine, text
from sqlalchemy.dialects.postgresql import UUID as PGUUID
from sqlalchemy.orm import DeclarativeBase, Mapped, Session, mapped_column, relationship, sessionmaker

from .config import get_settings


class Base(DeclarativeBase):
    pass


class VersionState(StrEnum):
    STAGING = "staging"
    ACTIVE = "active"
    FAILED = "failed"
    SUPERSEDED = "superseded"


class DatasetSourceType(StrEnum):
    MANAGED_FILE = "managed_file"
    POSTGRESQL = "postgresql"
    MYSQL = "mysql"


class DatabaseDialect(StrEnum):
    POSTGRESQL = "postgresql"
    MYSQL = "mysql"


class Tenant(Base):
    __tablename__ = "sq_tenants"

    id: Mapped[str] = mapped_column(String(128), primary_key=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=datetime.utcnow)


class Namespace(Base):
    __tablename__ = "sq_namespaces"
    __table_args__ = (UniqueConstraint("tenant_id", "name"),)

    id: Mapped[UUID] = mapped_column(PGUUID(as_uuid=True), primary_key=True, default=uuid4)
    tenant_id: Mapped[str] = mapped_column(ForeignKey("sq_tenants.id", ondelete="CASCADE"), index=True)
    name: Mapped[str] = mapped_column(String(128), nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=datetime.utcnow)


class DataSource(Base):
    __tablename__ = "sq_data_sources"
    __table_args__ = (UniqueConstraint("tenant_id", "namespace", "name"),)

    id: Mapped[UUID] = mapped_column(PGUUID(as_uuid=True), primary_key=True, default=uuid4)
    tenant_id: Mapped[str] = mapped_column(ForeignKey("sq_tenants.id", ondelete="CASCADE"), index=True)
    namespace: Mapped[str] = mapped_column(String(128), nullable=False, index=True)
    name: Mapped[str] = mapped_column(String(128), nullable=False)
    dialect: Mapped[str] = mapped_column(String(32), nullable=False)
    secret_ref: Mapped[str] = mapped_column(String(256), nullable=False)
    catalog: Mapped[str] = mapped_column(String(128), nullable=False)
    allowed_schemas: Mapped[list[str]] = mapped_column(JSON, default=list)
    allowed_tables: Mapped[list[dict]] = mapped_column(JSON, nullable=False)
    field_policies: Mapped[dict] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=datetime.utcnow)


class Dataset(Base):
    __tablename__ = "sq_datasets"
    __table_args__ = (UniqueConstraint("tenant_id", "namespace", "idempotency_key"),)

    id: Mapped[UUID] = mapped_column(PGUUID(as_uuid=True), primary_key=True, default=uuid4)
    tenant_id: Mapped[str] = mapped_column(String(128), nullable=False, index=True)
    namespace: Mapped[str] = mapped_column(String(128), nullable=False, index=True)
    idempotency_key: Mapped[str] = mapped_column(String(128), nullable=False)
    content_sha256: Mapped[str] = mapped_column(String(64), nullable=False)
    original_file_name: Mapped[str] = mapped_column(String(512), nullable=False)
    source_type: Mapped[str] = mapped_column(String(32), default=DatasetSourceType.MANAGED_FILE, nullable=False)
    data_source_id: Mapped[UUID | None] = mapped_column(
        ForeignKey("sq_data_sources.id", ondelete="RESTRICT"), nullable=True, index=True
    )
    active_version_id: Mapped[UUID | None] = mapped_column(PGUUID(as_uuid=True), nullable=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=datetime.utcnow)

    versions: Mapped[list["DatasetVersion"]] = relationship(back_populates="dataset")


class DatasetVersion(Base):
    __tablename__ = "sq_dataset_versions"

    id: Mapped[UUID] = mapped_column(PGUUID(as_uuid=True), primary_key=True, default=uuid4)
    dataset_id: Mapped[UUID] = mapped_column(ForeignKey("sq_datasets.id", ondelete="CASCADE"), index=True)
    version_number: Mapped[int] = mapped_column(Integer, nullable=False)
    state: Mapped[str] = mapped_column(String(32), default=VersionState.STAGING)
    profile: Mapped[dict] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=datetime.utcnow)
    activated_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))

    dataset: Mapped[Dataset] = relationship(back_populates="versions")
    tables: Mapped[list["DataTable"]] = relationship(back_populates="version")

    __table_args__ = (UniqueConstraint("dataset_id", "version_number"),)


class DataTable(Base):
    __tablename__ = "sq_tables"
    __table_args__ = (UniqueConstraint("version_id", "physical_schema", "physical_name"),)

    id: Mapped[UUID] = mapped_column(PGUUID(as_uuid=True), primary_key=True, default=uuid4)
    version_id: Mapped[UUID] = mapped_column(ForeignKey("sq_dataset_versions.id", ondelete="CASCADE"), index=True)
    sheet_name: Mapped[str] = mapped_column(String(512), nullable=False)
    physical_schema: Mapped[str] = mapped_column(String(63), nullable=False)
    physical_name: Mapped[str] = mapped_column(String(63), nullable=False)
    row_count: Mapped[int] = mapped_column(Integer, default=0)
    profile: Mapped[dict] = mapped_column(JSON, default=dict)

    version: Mapped[DatasetVersion] = relationship(back_populates="tables")
    columns: Mapped[list["DataColumn"]] = relationship(back_populates="table")


class DataColumn(Base):
    __tablename__ = "sq_columns"

    id: Mapped[UUID] = mapped_column(PGUUID(as_uuid=True), primary_key=True, default=uuid4)
    table_id: Mapped[UUID] = mapped_column(ForeignKey("sq_tables.id", ondelete="CASCADE"), index=True)
    ordinal: Mapped[int] = mapped_column(Integer, nullable=False)
    original_name: Mapped[str] = mapped_column(String(512), nullable=False)
    physical_name: Mapped[str] = mapped_column(String(63), nullable=False)
    data_type: Mapped[str] = mapped_column(String(128), nullable=False)
    nullable: Mapped[bool] = mapped_column(Boolean, default=True)
    profile: Mapped[dict] = mapped_column(JSON, default=dict)

    table: Mapped[DataTable] = relationship(back_populates="columns")
    values: Mapped[list["ColumnValue"]] = relationship(back_populates="column")

    __table_args__ = (UniqueConstraint("table_id", "physical_name"),)


class ColumnValue(Base):
    __tablename__ = "sq_column_values"

    id: Mapped[UUID] = mapped_column(PGUUID(as_uuid=True), primary_key=True, default=uuid4)
    column_id: Mapped[UUID] = mapped_column(ForeignKey("sq_columns.id", ondelete="CASCADE"), index=True)
    value: Mapped[str] = mapped_column(Text, nullable=False)
    frequency: Mapped[int] = mapped_column(Integer, nullable=False)
    indexed: Mapped[bool] = mapped_column(Boolean, default=False)

    column: Mapped[DataColumn] = relationship(back_populates="values")


class SchemaProfile(Base):
    __tablename__ = "sq_profiles"
    __table_args__ = (
        Index("ix_sq_profiles_version_kind", "version_id", "kind"),
        UniqueConstraint("version_id", "kind", "table_id", "column_id"),
    )

    id: Mapped[UUID] = mapped_column(PGUUID(as_uuid=True), primary_key=True, default=uuid4)
    version_id: Mapped[UUID] = mapped_column(ForeignKey("sq_dataset_versions.id", ondelete="CASCADE"), index=True)
    kind: Mapped[str] = mapped_column(String(32), nullable=False)
    table_id: Mapped[UUID | None] = mapped_column(ForeignKey("sq_tables.id", ondelete="CASCADE"), nullable=True)
    column_id: Mapped[UUID | None] = mapped_column(ForeignKey("sq_columns.id", ondelete="CASCADE"), nullable=True)
    payload: Mapped[dict] = mapped_column(JSON, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=datetime.utcnow)


class ImportJob(Base):
    __tablename__ = "sq_jobs"
    __table_args__ = (
        Index("ix_sq_jobs_tenant_created", "tenant_id", "created_at"),
        UniqueConstraint("dataset_id", "idempotency_key"),
    )

    id: Mapped[UUID] = mapped_column(PGUUID(as_uuid=True), primary_key=True, default=uuid4)
    tenant_id: Mapped[str] = mapped_column(String(128), nullable=False)
    dataset_id: Mapped[UUID] = mapped_column(ForeignKey("sq_datasets.id", ondelete="CASCADE"), index=True)
    version_id: Mapped[UUID] = mapped_column(ForeignKey("sq_dataset_versions.id", ondelete="CASCADE"), index=True)
    idempotency_key: Mapped[str | None] = mapped_column(String(128), nullable=True)
    state: Mapped[str] = mapped_column(String(32), default="queued")
    progress: Mapped[int] = mapped_column(Integer, default=0)
    error_code: Mapped[str | None] = mapped_column(String(128))
    error_message: Mapped[str | None] = mapped_column(Text)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=datetime.utcnow)
    updated_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=datetime.utcnow, onupdate=datetime.utcnow)


class QueryTrace(Base):
    __tablename__ = "sq_query_traces"
    __table_args__ = (Index("ix_sq_query_traces_tenant_created", "tenant_id", "created_at"),)

    id: Mapped[UUID] = mapped_column(PGUUID(as_uuid=True), primary_key=True, default=uuid4)
    tenant_id: Mapped[str] = mapped_column(String(128), nullable=False)
    namespace: Mapped[str] = mapped_column(String(128), nullable=False)
    dataset_ids: Mapped[list[str]] = mapped_column(JSON, nullable=False)
    sql_attempts: Mapped[list[str]] = mapped_column(JSON, nullable=False)
    error_codes: Mapped[list[str]] = mapped_column(JSON, nullable=False)
    model_calls: Mapped[int] = mapped_column(Integer, nullable=False)
    timings: Mapped[dict] = mapped_column(JSON, nullable=False)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=datetime.utcnow)


@lru_cache
def engine():
    return create_engine(
        get_settings().postgres_dsn.get_secret_value(), pool_pre_ping=True, hide_parameters=True  # type: ignore[union-attr]
    )


@lru_cache
def session_factory() -> sessionmaker[Session]:
    return sessionmaker(engine(), expire_on_commit=False)


def get_session() -> Generator[Session, None, None]:
    with session_factory()() as session:
        yield session


def initialize_database() -> None:
    with engine().begin() as connection:
        connection.execute(text("CREATE SCHEMA IF NOT EXISTS sq_data"))
        Base.metadata.create_all(connection)
