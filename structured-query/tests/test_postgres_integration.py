import os
from concurrent.futures import ThreadPoolExecutor
from io import BytesIO
from types import SimpleNamespace

import pandas as pd
import pytest
from sqlalchemy import create_engine, inspect, select, text
from sqlalchemy.orm import sessionmaker

from structured_query.database import Base, Dataset, DatasetVersion, VersionState
from structured_query.datasets import create_dataset
from structured_query.datasource_registry import refresh_datasource_profile, register_datasource
from structured_query.database import DataSource, ImportJob, Tenant
from structured_query.contracts import DatabaseDatasetCreate
from structured_query.versioning import activate_version


@pytest.fixture
def postgres_session():
    dsn = os.environ.get("STRUCTURED_QUERY_TEST_POSTGRES_DSN")
    if not dsn:
        pytest.skip("STRUCTURED_QUERY_TEST_POSTGRES_DSN is not configured")
    database = create_engine(dsn)
    with database.begin() as connection:
        connection.execute(text("CREATE SCHEMA IF NOT EXISTS sq_data"))
        Base.metadata.create_all(connection)
    factory = sessionmaker(database, expire_on_commit=False)
    try:
        with factory() as session:
            yield session
    finally:
        with database.begin() as connection:
            Base.metadata.drop_all(connection)
            connection.execute(text("DROP SCHEMA IF EXISTS sq_data CASCADE"))
        database.dispose()


def _workbook_bytes() -> bytes:
    stream = BytesIO()
    with pd.ExcelWriter(stream, engine="openpyxl") as writer:
        pd.DataFrame({"姓名": ["张三", "李四"], "学历": ["硕士", "本科"]}).to_excel(
            writer, sheet_name="人员清单", index=False
        )
        pd.DataFrame({"证书": ["软件测评师", "软件评测师"], "数量": [1, 2]}).to_excel(
            writer, sheet_name="证书", index=False
        )
    return stream.getvalue()


def test_idempotency_tenant_isolation_and_atomic_version_switch(postgres_session):
    first = create_dataset(
        postgres_session,
        tenant_id="tenant-a",
        namespace="people",
        idempotency_key="same-request",
        original_file_name="people.csv",
        content=b"name\nA\n",
    )
    duplicate = create_dataset(
        postgres_session,
        tenant_id="tenant-a",
        namespace="people",
        idempotency_key="same-request",
        original_file_name="people.csv",
        content=b"name\nA\n",
    )
    isolated = create_dataset(
        postgres_session,
        tenant_id="tenant-b",
        namespace="people",
        idempotency_key="same-request",
        original_file_name="people.csv",
        content=b"name\nA\n",
    )
    assert duplicate.created is False
    assert duplicate.dataset.id == first.dataset.id
    assert isolated.dataset.id != first.dataset.id

    activate_version(postgres_session, first.dataset.id, first.version.id)
    postgres_session.commit()
    second = DatasetVersion(dataset_id=first.dataset.id, version_number=2)
    postgres_session.add(second)
    postgres_session.flush()
    activate_version(postgres_session, first.dataset.id, second.id)
    postgres_session.commit()
    postgres_session.expire_all()

    dataset = postgres_session.get(Dataset, first.dataset.id)
    old_version = postgres_session.get(DatasetVersion, first.version.id)
    assert dataset is not None and dataset.active_version_id == second.id
    assert old_version is not None and old_version.state == VersionState.SUPERSEDED


def test_concurrent_file_idempotency_returns_one_dataset(postgres_session):
    factory = sessionmaker(postgres_session.get_bind(), expire_on_commit=False)

    def create_once(_):
        with factory() as session:
            return create_dataset(
                session,
                tenant_id="tenant-concurrent",
                namespace="same-file",
                idempotency_key="same-concurrent-request",
                original_file_name="same.csv",
                content=b"name\nA\n",
            )

    with ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(create_once, range(2)))
    assert len({item.dataset.id for item in results}) == 1
    assert sorted(item.created for item in results) == [False, True]


def test_concurrent_new_files_share_tenant_namespace_safely(postgres_session):
    factory = sessionmaker(postgres_session.get_bind(), expire_on_commit=False)

    def create_distinct(index):
        with factory() as session:
            return create_dataset(
                session,
                tenant_id="tenant-new-scope",
                namespace="new-scope",
                idempotency_key=f"distinct-request-{index}",
                original_file_name=f"file-{index}.csv",
                content=f"name\n{index}\n".encode(),
            )

    with ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(create_distinct, range(2)))
    assert len({item.dataset.id for item in results}) == 2
    assert all(item.created for item in results)


def test_concurrent_database_registration_returns_original_job(postgres_session):
    factory = sessionmaker(postgres_session.get_bind(), expire_on_commit=False)
    request = DatabaseDatasetCreate.model_validate({
        "kind": "database", "namespace": "concurrent-db", "name": "hr",
        "dialect": "postgresql", "secret_ref": "tenant/db", "catalog": "hr",
        "tables": [{"schema": "public", "table": "people"}],
    })

    def register_once(_):
        with factory() as session:
            return register_datasource(
                session, "tenant-db-concurrent", request, "same-database-request"
            )

    with ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(register_once, range(2)))
    assert len({item[0].id for item in results}) == 1
    assert len({item[2].id for item in results}) == 1
    assert sorted(item[3] for item in results) == [False, True]


def test_external_datasource_profile_refresh_creates_staging_version(postgres_session):
    postgres_session.add(Tenant(id="tenant-refresh"))
    postgres_session.flush()
    source = DataSource(
        tenant_id="tenant-refresh",
        namespace="hr",
        name="people-db",
        dialect="postgresql",
        secret_ref="tenant-refresh/people",
        catalog="people",
        allowed_schemas=["public"],
        allowed_tables=[{"schema": "public", "table": "people"}],
        field_policies={},
    )
    postgres_session.add(source)
    postgres_session.flush()
    dataset = Dataset(
        tenant_id="tenant-refresh",
        namespace="hr",
        idempotency_key="register-db",
        content_sha256="0" * 64,
        original_file_name="people-db",
        source_type="postgresql",
        data_source_id=source.id,
    )
    first = DatasetVersion(dataset=dataset, version_number=1, state=VersionState.ACTIVE)
    postgres_session.add_all([dataset, first])
    postgres_session.flush()
    dataset.active_version_id = first.id
    postgres_session.commit()

    _, refreshed, job, created = refresh_datasource_profile(
        postgres_session, "tenant-refresh", dataset.id, "refresh-once"
    )

    assert refreshed.version_number == 2
    assert refreshed.state == VersionState.STAGING
    assert postgres_session.get(ImportJob, job.id) is not None
    assert postgres_session.get(Dataset, dataset.id).active_version_id == first.id
    _, duplicate_version, duplicate_job, duplicate_created = refresh_datasource_profile(
        postgres_session, "tenant-refresh", dataset.id, "refresh-once"
    )
    assert created is True and duplicate_created is False
    assert duplicate_version.id == refreshed.id and duplicate_job.id == job.id


def test_older_profile_completion_cannot_roll_back_newer_version(postgres_session):
    created = create_dataset(
        postgres_session,
        tenant_id="tenant-order",
        namespace="ordered",
        idempotency_key="initial-order",
        original_file_name="ordered.csv",
        content=b"value\n1\n",
    )
    activate_version(postgres_session, created.dataset.id, created.version.id)
    newer = DatasetVersion(dataset_id=created.dataset.id, version_number=3)
    older = DatasetVersion(dataset_id=created.dataset.id, version_number=2)
    postgres_session.add_all([older, newer])
    postgres_session.commit()

    activate_version(postgres_session, created.dataset.id, older.id)
    postgres_session.commit()
    assert postgres_session.get(Dataset, created.dataset.id).active_version_id == created.version.id
    assert postgres_session.get(DatasetVersion, older.id).state == VersionState.SUPERSEDED

    activate_version(postgres_session, created.dataset.id, newer.id)
    postgres_session.commit()
    assert postgres_session.get(Dataset, created.dataset.id).active_version_id == newer.id


def test_failed_staging_transaction_keeps_active_version_and_drops_table(postgres_session):
    created = create_dataset(
        postgres_session,
        tenant_id="tenant-a",
        namespace="rollback",
        idempotency_key="rollback-request",
        original_file_name="rollback.csv",
        content=b"value\n1\n",
    )
    activate_version(postgres_session, created.dataset.id, created.version.id)
    postgres_session.commit()

    postgres_session.execute(text("CREATE TABLE sq_data.staging_failure (value integer)"))
    postgres_session.add(DatasetVersion(dataset_id=created.dataset.id, version_number=2))
    postgres_session.rollback()

    postgres_session.expire_all()
    dataset = postgres_session.scalar(select(Dataset).where(Dataset.id == created.dataset.id))
    assert dataset is not None and dataset.active_version_id == created.version.id
    assert not inspect(postgres_session.connection()).has_table("staging_failure", schema="sq_data")


def test_multisheet_import_persists_rows_profiles_and_switches_active(postgres_session, monkeypatch):
    from structured_query import import_tasks
    from structured_query.database import DataColumn, DataTable, ImportJob, SchemaProfile

    content = _workbook_bytes()
    created = create_dataset(
        postgres_session,
        tenant_id="tenant-a",
        namespace="workbook",
        idempotency_key="workbook-request",
        original_file_name="workbook.xlsx",
        content=content,
    )
    factory = sessionmaker(postgres_session.get_bind(), expire_on_commit=False)
    indexed = []
    deleted_objects = []
    monkeypatch.setattr(import_tasks, "session_factory", lambda: factory)
    monkeypatch.setattr(import_tasks, "download_bytes", lambda _: content)
    monkeypatch.setattr(import_tasks, "delete_version", lambda _tenant, _version: None)
    monkeypatch.setattr(import_tasks, "index_documents", lambda documents: indexed.extend(documents))
    monkeypatch.setattr(import_tasks, "delete_object", deleted_objects.append)

    import_tasks._import_dataset(SimpleNamespace(retry=lambda **kwargs: None), str(created.job.id), "object", "workbook.xlsx")
    postgres_session.expire_all()

    dataset = postgres_session.get(Dataset, created.dataset.id)
    job = postgres_session.get(ImportJob, created.job.id)
    tables = list(postgres_session.scalars(select(DataTable).where(DataTable.version_id == created.version.id)))
    profiles = list(
        postgres_session.scalars(select(SchemaProfile).where(SchemaProfile.version_id == created.version.id))
    )
    columns = list(
        postgres_session.scalars(select(DataColumn).where(DataColumn.table_id.in_([table.id for table in tables])))
    )
    assert dataset is not None and dataset.active_version_id == created.version.id
    assert job is not None and job.state == "completed" and job.progress == 100
    assert len(tables) == 2 and sum(table.row_count for table in tables) == 4
    assert len(columns) == 4
    assert len(profiles) == 6
    assert indexed and deleted_objects == ["object"]
