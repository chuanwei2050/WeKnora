"""persist bounded structured query traces

Revision ID: 0004
Revises: 0003
"""

from alembic import op
import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

revision = "0004"
down_revision = "0003"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "sq_query_traces",
        sa.Column("id", postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("tenant_id", sa.String(128), nullable=False),
        sa.Column("namespace", sa.String(128), nullable=False),
        sa.Column("dataset_ids", sa.JSON(), nullable=False),
        sa.Column("sql_attempts", sa.JSON(), nullable=False),
        sa.Column("error_codes", sa.JSON(), nullable=False),
        sa.Column("model_calls", sa.Integer(), nullable=False),
        sa.Column("timings", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_index("ix_sq_query_traces_tenant_created", "sq_query_traces", ["tenant_id", "created_at"])


def downgrade() -> None:
    op.drop_table("sq_query_traces")
