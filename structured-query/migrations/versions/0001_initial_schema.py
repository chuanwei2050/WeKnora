"""Initial structured query metadata schema.

Revision ID: 0001
Revises:
"""

from typing import Sequence

from alembic import op
import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

revision: str = "0001"
down_revision: str | None = None
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.execute("CREATE SCHEMA IF NOT EXISTS sq_data")
    op.create_table(
        "sq_tenants",
        sa.Column("id", sa.String(128), primary_key=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_table(
        "sq_namespaces",
        sa.Column("id", postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("tenant_id", sa.String(128), sa.ForeignKey("sq_tenants.id", ondelete="CASCADE"), nullable=False),
        sa.Column("name", sa.String(128), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.UniqueConstraint("tenant_id", "name"),
    )
    op.create_index("ix_sq_namespaces_tenant_id", "sq_namespaces", ["tenant_id"])
    op.create_table(
        "sq_datasets",
        sa.Column("id", postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("tenant_id", sa.String(128), nullable=False),
        sa.Column("namespace", sa.String(128), nullable=False),
        sa.Column("idempotency_key", sa.String(128), nullable=False),
        sa.Column("content_sha256", sa.String(64), nullable=False),
        sa.Column("original_file_name", sa.String(512), nullable=False),
        sa.Column("active_version_id", postgresql.UUID(as_uuid=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.UniqueConstraint("tenant_id", "namespace", "idempotency_key"),
    )
    op.create_index("ix_sq_datasets_tenant_id", "sq_datasets", ["tenant_id"])
    op.create_index("ix_sq_datasets_namespace", "sq_datasets", ["namespace"])
    op.create_table(
        "sq_dataset_versions",
        sa.Column("id", postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("dataset_id", postgresql.UUID(as_uuid=True), sa.ForeignKey("sq_datasets.id", ondelete="CASCADE"), nullable=False),
        sa.Column("version_number", sa.Integer(), nullable=False),
        sa.Column("state", sa.String(32), nullable=False),
        sa.Column("profile", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("activated_at", sa.DateTime(timezone=True), nullable=True),
        sa.UniqueConstraint("dataset_id", "version_number"),
    )
    op.create_index("ix_sq_dataset_versions_dataset_id", "sq_dataset_versions", ["dataset_id"])
    op.create_table(
        "sq_tables",
        sa.Column("id", postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("version_id", postgresql.UUID(as_uuid=True), sa.ForeignKey("sq_dataset_versions.id", ondelete="CASCADE"), nullable=False),
        sa.Column("sheet_name", sa.String(512), nullable=False),
        sa.Column("physical_schema", sa.String(63), nullable=False),
        sa.Column("physical_name", sa.String(63), nullable=False, unique=True),
        sa.Column("row_count", sa.Integer(), nullable=False),
        sa.Column("profile", sa.JSON(), nullable=False),
    )
    op.create_index("ix_sq_tables_version_id", "sq_tables", ["version_id"])
    op.create_table(
        "sq_columns",
        sa.Column("id", postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("table_id", postgresql.UUID(as_uuid=True), sa.ForeignKey("sq_tables.id", ondelete="CASCADE"), nullable=False),
        sa.Column("ordinal", sa.Integer(), nullable=False),
        sa.Column("original_name", sa.String(512), nullable=False),
        sa.Column("physical_name", sa.String(63), nullable=False),
        sa.Column("data_type", sa.String(128), nullable=False),
        sa.Column("nullable", sa.Boolean(), nullable=False),
        sa.Column("profile", sa.JSON(), nullable=False),
        sa.UniqueConstraint("table_id", "physical_name"),
    )
    op.create_index("ix_sq_columns_table_id", "sq_columns", ["table_id"])
    op.create_table(
        "sq_column_values",
        sa.Column("id", postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("column_id", postgresql.UUID(as_uuid=True), sa.ForeignKey("sq_columns.id", ondelete="CASCADE"), nullable=False),
        sa.Column("value", sa.Text(), nullable=False),
        sa.Column("frequency", sa.Integer(), nullable=False),
        sa.Column("indexed", sa.Boolean(), nullable=False),
    )
    op.create_index("ix_sq_column_values_column_id", "sq_column_values", ["column_id"])
    op.create_table(
        "sq_profiles",
        sa.Column("id", postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("version_id", postgresql.UUID(as_uuid=True), sa.ForeignKey("sq_dataset_versions.id", ondelete="CASCADE"), nullable=False),
        sa.Column("kind", sa.String(32), nullable=False),
        sa.Column("table_id", postgresql.UUID(as_uuid=True), sa.ForeignKey("sq_tables.id", ondelete="CASCADE"), nullable=True),
        sa.Column("column_id", postgresql.UUID(as_uuid=True), sa.ForeignKey("sq_columns.id", ondelete="CASCADE"), nullable=True),
        sa.Column("payload", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.UniqueConstraint("version_id", "kind", "table_id", "column_id"),
    )
    op.create_index("ix_sq_profiles_version_id", "sq_profiles", ["version_id"])
    op.create_index("ix_sq_profiles_version_kind", "sq_profiles", ["version_id", "kind"])
    op.create_table(
        "sq_jobs",
        sa.Column("id", postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("tenant_id", sa.String(128), nullable=False),
        sa.Column("dataset_id", postgresql.UUID(as_uuid=True), sa.ForeignKey("sq_datasets.id", ondelete="CASCADE"), nullable=False),
        sa.Column("version_id", postgresql.UUID(as_uuid=True), sa.ForeignKey("sq_dataset_versions.id", ondelete="CASCADE"), nullable=False),
        sa.Column("state", sa.String(32), nullable=False),
        sa.Column("progress", sa.Integer(), nullable=False),
        sa.Column("error_code", sa.String(128), nullable=True),
        sa.Column("error_message", sa.Text(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_index("ix_sq_jobs_dataset_id", "sq_jobs", ["dataset_id"])
    op.create_index("ix_sq_jobs_version_id", "sq_jobs", ["version_id"])
    op.create_index("ix_sq_jobs_tenant_created", "sq_jobs", ["tenant_id", "created_at"])


def downgrade() -> None:
    for table in (
        "sq_jobs",
        "sq_profiles",
        "sq_column_values",
        "sq_columns",
        "sq_tables",
        "sq_dataset_versions",
        "sq_datasets",
        "sq_namespaces",
        "sq_tenants",
    ):
        op.drop_table(table)
    op.execute("DROP SCHEMA IF EXISTS sq_data CASCADE")
