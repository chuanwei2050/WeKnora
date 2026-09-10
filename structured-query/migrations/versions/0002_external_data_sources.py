"""Add zero-copy PostgreSQL and MySQL data sources.

Revision ID: 0002
Revises: 0001
"""

from typing import Sequence

from alembic import op
import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

revision: str = "0002"
down_revision: str | None = "0001"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "sq_data_sources",
        sa.Column("id", postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("tenant_id", sa.String(128), sa.ForeignKey("sq_tenants.id", ondelete="CASCADE"), nullable=False),
        sa.Column("namespace", sa.String(128), nullable=False),
        sa.Column("name", sa.String(128), nullable=False),
        sa.Column("dialect", sa.String(32), nullable=False),
        sa.Column("secret_ref", sa.String(256), nullable=False),
        sa.Column("catalog", sa.String(128), nullable=False),
        sa.Column("allowed_schemas", sa.JSON(), nullable=False),
        sa.Column("allowed_tables", sa.JSON(), nullable=False),
        sa.Column("field_policies", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.UniqueConstraint("tenant_id", "namespace", "name"),
    )
    op.create_index("ix_sq_data_sources_tenant_id", "sq_data_sources", ["tenant_id"])
    op.create_index("ix_sq_data_sources_namespace", "sq_data_sources", ["namespace"])
    op.add_column(
        "sq_datasets",
        sa.Column("source_type", sa.String(32), nullable=False, server_default="managed_file"),
    )
    op.add_column(
        "sq_datasets",
        sa.Column("data_source_id", postgresql.UUID(as_uuid=True), nullable=True),
    )
    op.create_foreign_key(
        "fk_sq_datasets_data_source_id",
        "sq_datasets",
        "sq_data_sources",
        ["data_source_id"],
        ["id"],
        ondelete="RESTRICT",
    )
    op.create_index("ix_sq_datasets_data_source_id", "sq_datasets", ["data_source_id"])
    op.alter_column("sq_datasets", "source_type", server_default=None)


def downgrade() -> None:
    op.drop_index("ix_sq_datasets_data_source_id", table_name="sq_datasets")
    op.drop_constraint("fk_sq_datasets_data_source_id", "sq_datasets", type_="foreignkey")
    op.drop_column("sq_datasets", "data_source_id")
    op.drop_column("sq_datasets", "source_type")
    op.drop_table("sq_data_sources")
