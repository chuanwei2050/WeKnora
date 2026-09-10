from hashlib import sha256
import json
from uuid import uuid4

from sqlalchemy import func, select, text
from sqlalchemy.orm import Session

from .contracts import DatabaseDatasetCreate
from .database import DataSource, Dataset, DatasetVersion, ImportJob, Namespace, Tenant


def refresh_datasource_profile(
    session: Session, tenant_id: str, dataset_id, idempotency_key: str
) -> tuple[Dataset, DatasetVersion, ImportJob, bool]:
    dataset = session.scalar(
        select(Dataset).where(
            Dataset.id == dataset_id, Dataset.tenant_id == tenant_id
        ).with_for_update()
    )
    if dataset is None or dataset.data_source_id is None:
        raise ValueError("database_dataset_not_found")
    existing_job = session.scalar(select(ImportJob).where(
        ImportJob.dataset_id == dataset.id,
        ImportJob.idempotency_key == idempotency_key,
    ))
    if existing_job is not None:
        version = session.get(DatasetVersion, existing_job.version_id)
        if version is None:
            raise ValueError("dataset_metadata_incomplete")
        return dataset, version, existing_job, False
    latest = session.scalar(
        select(func.max(DatasetVersion.version_number)).where(DatasetVersion.dataset_id == dataset.id)
    ) or 0
    version = DatasetVersion(id=uuid4(), dataset=dataset, version_number=latest + 1)
    session.add(version)
    session.flush()
    job = ImportJob(
        id=uuid4(), tenant_id=tenant_id, dataset_id=dataset.id,
        version_id=version.id, idempotency_key=idempotency_key,
    )
    session.add(job)
    session.commit()
    return dataset, version, job, True


def register_datasource(
    session: Session, tenant_id: str, request: DatabaseDatasetCreate, idempotency_key: str
) -> tuple[Dataset, DatasetVersion, ImportJob, bool]:
    payload = request.model_dump(mode="json")
    digest = sha256(json.dumps(payload, sort_keys=True).encode()).hexdigest()
    session.execute(
        text("SELECT pg_advisory_xact_lock(hashtextextended(:key, 0))"),
        {"key": f"dataset-scope:{tenant_id}:{request.namespace}"},
    )
    existing = session.scalar(
        select(Dataset).where(
            Dataset.tenant_id == tenant_id,
            Dataset.namespace == request.namespace,
            Dataset.idempotency_key == idempotency_key,
        )
    )
    if existing is not None:
        if existing.content_sha256 != digest:
            raise ValueError("idempotency_payload_mismatch")
        version = session.scalar(select(DatasetVersion).where(
            DatasetVersion.dataset_id == existing.id,
            DatasetVersion.version_number == 1,
        ))
        job = session.scalar(select(ImportJob).where(ImportJob.version_id == version.id)) if version else None
        if version is None or job is None:
            raise ValueError("dataset_metadata_incomplete")
        return existing, version, job, False
    if session.get(Tenant, tenant_id) is None:
        session.add(Tenant(id=tenant_id))
    if session.scalar(select(Namespace).where(Namespace.tenant_id == tenant_id, Namespace.name == request.namespace)) is None:
        session.add(Namespace(tenant_id=tenant_id, name=request.namespace))
    if session.scalar(select(DataSource).where(
        DataSource.tenant_id == tenant_id,
        DataSource.namespace == request.namespace,
        DataSource.name == request.name,
    )) is not None:
        raise ValueError("datasource_already_exists")
    source = DataSource(
        id=uuid4(), tenant_id=tenant_id, namespace=request.namespace, name=request.name,
        dialect=request.dialect, secret_ref=request.secret_ref, catalog=request.catalog,
        allowed_schemas=sorted({table.schema_name for table in request.tables}),
        allowed_tables=[table.model_dump(by_alias=True) for table in request.tables],
        field_policies={f"{p.schema_name}.{p.table}.{p.column}": {"persist_values": p.persist_values} for p in request.field_policies},
    )
    dataset = Dataset(
        id=uuid4(), tenant_id=tenant_id, namespace=request.namespace, idempotency_key=idempotency_key,
        content_sha256=digest, original_file_name=request.name, source_type=request.dialect, data_source_id=source.id,
    )
    version = DatasetVersion(id=uuid4(), dataset=dataset, version_number=1)
    session.add_all([source, dataset, version])
    session.flush()
    job = ImportJob(id=uuid4(), tenant_id=tenant_id, dataset_id=dataset.id, version_id=version.id)
    session.add(job)
    session.commit()
    return dataset, version, job, True
