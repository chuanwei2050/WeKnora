"""Add indexed substring search for Profile names and values.

Revision ID: 0006
Revises: 0005
"""

from typing import Sequence

from alembic import op

revision: str = "0006"
down_revision: str | None = "0005"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.execute("CREATE EXTENSION IF NOT EXISTS pg_trgm")
    op.execute(
        "CREATE INDEX IF NOT EXISTS ix_sq_column_values_value_trgm "
        "ON sq_column_values USING gin (lower(value) gin_trgm_ops)"
    )
    op.execute(
        "CREATE INDEX IF NOT EXISTS ix_sq_columns_original_name_trgm "
        "ON sq_columns USING gin (lower(original_name) gin_trgm_ops)"
    )


def downgrade() -> None:
    op.execute("DROP INDEX IF EXISTS ix_sq_columns_original_name_trgm")
    op.execute("DROP INDEX IF EXISTS ix_sq_column_values_value_trgm")
