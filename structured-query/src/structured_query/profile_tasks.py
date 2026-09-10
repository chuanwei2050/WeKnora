from uuid import UUID, uuid4
from concurrent.futures import ThreadPoolExecutor

from .config import get_settings
from .database import ColumnValue, DataColumn, DataSource, DataTable, Dataset, DatasetVersion, ImportJob, SchemaProfile, VersionState, session_factory
from .datasource_adapters import TableRef, create_adapter
from .task_lock import import_job_lock
from .vector_store import ProfileDocument, delete_version, index_documents
from .versioning import activate_version
from .worker import celery_app


@celery_app.task(name="structured_query.profile_datasource", bind=True, max_retries=2)
def profile_datasource(self, job_id: str) -> None:
    with import_job_lock(job_id) as acquired:
        if acquired:
            _profile_datasource(self, job_id)


def _profile_datasource(task, job_id: str) -> None:
    should_retry = False
    with session_factory()() as session:
        job = session.get(ImportJob, UUID(job_id))
        if job is None or job.state == "completed":
            return
        job.state, job.progress = "running", 5
        session.commit()
        version = session.get(DatasetVersion, job.version_id)
        dataset = session.get(Dataset, job.dataset_id)
        source = session.get(DataSource, dataset.data_source_id) if dataset else None
        try:
            if version is None or dataset is None or source is None:
                raise ValueError("datasource_metadata_incomplete")
            allowed = {TableRef(row["schema"], row["table"]) for row in source.allowed_tables}
            adapter = create_adapter(source.dialect, source.secret_ref, allowed)
            settings = get_settings()
            documents: list[ProfileDocument] = []
            for profile in adapter.inspect_tables():
                column_lines = [f"  ({raw['name']}, {raw['type']}, nullable={str(bool(raw.get('nullable', True))).lower()})" for raw in profile.columns]
                mschema = "\n".join([f"【DB_ID】 {source.catalog}", f"# Table: {profile.table.schema}.{profile.table.name}", "[", *column_lines, "]"])
                table = DataTable(
                    id=uuid4(), version_id=version.id, sheet_name=profile.table.name,
                    physical_schema=profile.table.schema, physical_name=profile.table.name, row_count=0,
                    profile={"dialect": source.dialect, "mschema": mschema, "primary_key": list(profile.primary_key), "foreign_keys": list(profile.foreign_keys)},
                )
                session.add(table); session.flush()
                session.add(SchemaProfile(version_id=version.id, kind="table", table_id=table.id, payload=table.profile))
                documents.append(ProfileDocument(dataset.tenant_id, dataset.namespace, version.id, "table", table.id, None, f"{profile.table.schema}.{profile.table.name}"))
                def profile_one(raw):
                    persist = source.field_policies.get(
                        f'{profile.table.schema}.{profile.table.name}.{raw["name"]}', {}
                    ).get("persist_values", False)
                    return raw["name"], persist, adapter.profile_column(
                        profile.table, raw["name"], persist
                    )

                with ThreadPoolExecutor(
                    max_workers=min(settings.datasource_profile_concurrency, len(profile.columns) or 1),
                    thread_name_prefix="datasource-profile",
                ) as executor:
                    statistics_by_name = {
                        name: (persist, statistics)
                        for name, persist, statistics in executor.map(profile_one, profile.columns)
                    }
                projected_documents = (
                    len(documents) + len(profile.columns)
                    + sum(len(statistics["values"]) for _, statistics in statistics_by_name.values())
                )
                if projected_documents > settings.max_profile_documents_per_version:
                    raise ValueError("too_many_profile_documents")
                for ordinal, raw in enumerate(profile.columns, 1):
                    persist_values, statistics = statistics_by_name[raw["name"]]
                    table.row_count = max(table.row_count, statistics["row_count"])
                    column = DataColumn(
                        id=uuid4(), table_id=table.id, ordinal=ordinal, original_name=raw["name"],
                        physical_name=raw["name"], data_type=str(raw["type"]), nullable=bool(raw.get("nullable", True)),
                        profile={"persist_values": persist_values, **statistics},
                    )
                    session.add(column); session.flush()
                    session.add(SchemaProfile(version_id=version.id, kind="column", table_id=table.id, column_id=column.id, payload=column.profile))
                    documents.append(ProfileDocument(dataset.tenant_id, dataset.namespace, version.id, "column", table.id, column.id, f"{profile.table.name} {raw['name']} {raw['type']}"))
                    for item in statistics["values"]:
                        session.add(ColumnValue(column_id=column.id, value=item["value"], frequency=item["frequency"], indexed=True))
                        documents.append(ProfileDocument(dataset.tenant_id, dataset.namespace, version.id, "value", table.id, column.id, f"{raw['name']}: {item['value']}", item["frequency"]))
            delete_version(dataset.tenant_id, version.id); index_documents(documents)
            activate_version(session, dataset.id, version.id)
            version.profile = {"table_count": len(allowed), "dialect": source.dialect}
            job.state, job.progress = "completed", 100
            session.commit()
        except Exception:
            session.rollback()
            job = session.get(ImportJob, UUID(job_id))
            if job:
                job.state, job.error_code, job.error_message = "failed", "profile_failed", "数据源画像失败"
                failed = session.get(DatasetVersion, job.version_id)
                if failed: failed.state = VersionState.FAILED
                session.commit()
            if version:
                try: delete_version(dataset.tenant_id, version.id)
                except Exception: pass
            should_retry = True
    if should_retry:
        raise task.retry(exc=RuntimeError("profile_failed")) from None
