"""Make external profile refreshes idempotent.

Revision ID: 0005
Revises: 0004
"""

from typing import Sequence

from alembic import op
import sqlalchemy as sa

revision: str = "0005"
down_revision: str | None = "0004"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column("sq_jobs", sa.Column("idempotency_key", sa.String(128), nullable=True))
    op.create_unique_constraint(
        "uq_sq_jobs_dataset_refresh_idempotency",
        "sq_jobs",
        ["dataset_id", "idempotency_key"],
    )


def downgrade() -> None:
    op.drop_constraint(
        "uq_sq_jobs_dataset_refresh_idempotency", "sq_jobs", type_="unique"
    )
    op.drop_column("sq_jobs", "idempotency_key")
