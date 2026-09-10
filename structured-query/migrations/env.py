from logging.config import fileConfig

from alembic import context
from sqlalchemy import create_engine

from structured_query.config import postgres_dsn_from_environ
from structured_query.database import Base

config = context.config
if config.config_file_name is not None:
    fileConfig(config.config_file_name)
postgres_dsn = postgres_dsn_from_environ()
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
