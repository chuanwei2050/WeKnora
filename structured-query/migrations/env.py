from logging.config import fileConfig
import os

from alembic import context
from sqlalchemy import create_engine

from structured_query.database import Base

config = context.config
if config.config_file_name is not None:
    fileConfig(config.config_file_name)
postgres_dsn = os.environ.get("STRUCTURED_QUERY_POSTGRES_DSN")
if not postgres_dsn:
    raise RuntimeError("STRUCTURED_QUERY_POSTGRES_DSN is required for migrations")
config.set_main_option("sqlalchemy.url", postgres_dsn.replace("%", "%%"))
target_metadata = Base.metadata


def run_migrations_offline() -> None:
    context.configure(url=config.get_main_option("sqlalchemy.url"), target_metadata=target_metadata, literal_binds=True)
    with context.begin_transaction():
        context.run_migrations()


def run_migrations_online() -> None:
    migration_engine = create_engine(postgres_dsn, pool_pre_ping=True)
    with migration_engine.connect() as connection:
        context.configure(connection=connection, target_metadata=target_metadata)
        with context.begin_transaction():
            context.run_migrations()
    migration_engine.dispose()


run_migrations_offline() if context.is_offline_mode() else run_migrations_online()
