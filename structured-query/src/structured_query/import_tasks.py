from io import BytesIO
from dataclasses import replace
from uuid import UUID, uuid4

from sqlalchemy import inspect

from .config import get_settings
from .database import ColumnValue, DataColumn, DataTable, Dataset, DatasetVersion, ImportJob, SchemaProfile, VersionState, session_factory
from .ingestion import parse_tabular_file
from .object_store import delete_object, download_bytes
from .profile import profile_frame, profile_to_dict, to_mschema_context
from .task_lock import import_job_lock
from .worker import celery_app
from .vector_store import ProfileDocument, delete_version, index_documents
from .versioning import activate_version


@celery_app.task(name="structured_query.import_dataset", bind=True, max_retries=2)
def import_dataset(self, job_id: str, object_key: str, file_name: str) -> None:
    with import_job_lock(job_id) as acquired:
        if not acquired:
            return
        _import_dataset(self, job_id, object_key, file_name)


def _import_dataset(task, job_id: str, object_key: str, file_name: str) -> None:
    settings = get_settings()
    factory = session_factory()
    with factory() as session:
        job = session.get(ImportJob, UUID(job_id))
        if job is None:
            return
        if job.state == "completed":
            return
        job.state, job.progress = "running", 5
        session.commit()
        try:
            content = download_bytes(object_key)
            parsed = parse_tabular_file(
                file_name,
                BytesIO(content),
                max_sheets=settings.max_import_sheets,
                max_rows=settings.max_import_rows_per_sheet,
                max_columns=settings.max_import_columns,
                max_excel_uncompressed_bytes=settings.max_excel_uncompressed_bytes,
                max_excel_archive_entries=settings.max_excel_archive_entries,
            )
            if len(parsed) > settings.max_import_sheets:
                raise ValueError("too_many_sheets")
            if any(len(item.frame) > settings.max_import_rows_per_sheet for item in parsed):
                raise ValueError("too_many_rows")
            if any(len(item.frame.columns) > settings.max_import_columns for item in parsed):
                raise ValueError("too_many_columns")
            version = session.get(DatasetVersion, job.version_id)
            dataset = session.get(Dataset, job.dataset_id)
            assert version is not None and dataset is not None
            version.state = VersionState.STAGING
            documents: list[ProfileDocument] = []
            total_rows = 0
            for index, parsed_table in enumerate(parsed, start=1):
                table_id = uuid4()
                physical_name = f"d_{version.id.hex[:16]}_{index:03d}"
                # Stay below PostgreSQL's bind-parameter ceiling for wide sheets.
                insert_chunksize = max(1, min(1_000, 60_000 // len(parsed_table.frame.columns)))
                parsed_table.frame.to_sql(
                    physical_name,
                    session.connection(),
                    schema="sq_data",
                    if_exists="fail",
                    index=False,
                    method="multi",
                    chunksize=insert_chunksize,
                )
                column_profiles = profile_frame(
                    parsed_table.frame,
                    parsed_table.column_mapping,
                    settings.low_cardinality_limit,
                    settings.high_cardinality_sample,
                )
                projected_documents = (
                    len(documents) + 1 + len(column_profiles)
                    + sum(len(column.values) for column in column_profiles)
                )
                if projected_documents > settings.max_profile_documents_per_version:
                    raise ValueError("too_many_profile_documents")
                reflected = {
                    column["name"]: str(column["type"])
                    for column in inspect(session.connection()).get_columns(physical_name, schema="sq_data")
                }
                column_profiles = [
                    replace(column, data_type=reflected.get(column.physical_name, column.data_type))
                    for column in column_profiles
                ]
                total_rows += len(parsed_table.frame)
                table_profile = {
                    "mschema": to_mschema_context(
                        dataset.namespace, physical_name, parsed_table.sheet_name, column_profiles
                    ),
                    "row_count": len(parsed_table.frame),
                    "columns": [profile_to_dict(column) for column in column_profiles],
                    "primary_key": inspect(session.connection()).get_pk_constraint(
                        physical_name, schema="sq_data"
                    ).get("constrained_columns", []),
                    "foreign_keys": inspect(session.connection()).get_foreign_keys(
                        physical_name, schema="sq_data"
                    ),
                }
                table = DataTable(
                    id=table_id,
                    version_id=version.id,
                    sheet_name=parsed_table.sheet_name,
                    physical_schema="sq_data",
                    physical_name=physical_name,
                    row_count=len(parsed_table.frame),
                    profile=table_profile,
                )
                session.add(table)
                session.flush()
                session.add(
                    SchemaProfile(
                        version_id=version.id,
                        kind="table",
                        table_id=table.id,
                        payload=table_profile,
                    )
                )
                documents.append(
                    ProfileDocument(
                        tenant_id=dataset.tenant_id,
                        namespace=dataset.namespace,
                        version_id=version.id,
                        kind="table",
                        table_id=table.id,
                        column_id=None,
                        text=f"{parsed_table.sheet_name}\n{table.profile['mschema']}",
                    )
                )
                for ordinal, column_profile in enumerate(column_profiles, start=1):
                    column_id = uuid4()
                    column = DataColumn(
                        id=column_id,
                        table=table,
                        ordinal=ordinal,
                        original_name=column_profile.original_name,
                        physical_name=column_profile.physical_name,
                        data_type=column_profile.data_type,
                        nullable=column_profile.nullable,
                        profile=profile_to_dict(column_profile),
                    )
                    session.add(column)
                    # Flush before dependent rows so insertmany cannot outrun sq_columns FK checks.
                    session.flush()
                    session.add(
                        SchemaProfile(
                            version_id=version.id,
                            kind="column",
                            table_id=table.id,
                            column_id=column_id,
                            payload=profile_to_dict(column_profile),
                        )
                    )
                    documents.append(
                        ProfileDocument(
                            tenant_id=dataset.tenant_id,
                            namespace=dataset.namespace,
                            version_id=version.id,
                            kind="column",
                            table_id=table.id,
                            column_id=column_id,
                            text=f"{parsed_table.sheet_name} {column.original_name} {column.data_type}",
                        )
                    )
                    for value in column_profile.values:
                        session.add(ColumnValue(column_id=column_id, value=value.value, frequency=value.frequency, indexed=True))
                        documents.append(
                            ProfileDocument(
                                tenant_id=dataset.tenant_id,
                                namespace=dataset.namespace,
                                version_id=version.id,
                                kind="value",
                                table_id=table.id,
                                column_id=column_id,
                                text=f"{column.original_name}: {value.value}",
                                frequency=value.frequency,
                            )
                        )
                job.progress = min(85, 10 + int(index / len(parsed) * 75))
                session.flush()
            version.profile = {"table_count": len(parsed), "row_count": total_rows}
            delete_version(dataset.tenant_id, version.id)
            index_documents(documents)
            activate_version(session, dataset.id, version.id)
            job.state, job.progress = "completed", 100
            session.commit()
        except Exception as error:
            session.rollback()
            job = session.get(ImportJob, UUID(job_id))
            version = session.get(DatasetVersion, job.version_id) if job else None
            if job is not None:
                job.state = "failed"
                job.error_code = "import_failed"
                job.error_message = f"文件解析或结构化入库失败: {type(error).__name__}: {error}"
            if version is not None:
                version.state = VersionState.FAILED
            session.commit()
            try:
                delete_version(dataset.tenant_id, version.id) if version is not None else None
            except Exception:
                pass
            raise task.retry(exc=RuntimeError("import_failed")) from error
        try:
            delete_object(object_key)
        except Exception:
            # The active version is already durable. Staging cleanup is best-effort
            # and must never turn a successful import into a failed/retried one.
            pass
