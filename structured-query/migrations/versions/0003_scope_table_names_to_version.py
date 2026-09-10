"""scope table names to a dataset version

Revision ID: 0003
Revises: 0002
"""

from alembic import op

revision = "0003"
down_revision = "0002"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.drop_constraint("sq_tables_physical_name_key", "sq_tables", type_="unique")
    op.create_unique_constraint(
        "uq_sq_tables_version_schema_name",
        "sq_tables",
        ["version_id", "physical_schema", "physical_name"],
    )


def downgrade() -> None:
    op.drop_constraint("uq_sq_tables_version_schema_name", "sq_tables", type_="unique")
    op.create_unique_constraint("sq_tables_physical_name_key", "sq_tables", ["physical_name"])
