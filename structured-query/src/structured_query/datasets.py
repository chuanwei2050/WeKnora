from dataclasses import dataclass
from hashlib import sha256
from uuid import uuid4

from sqlalchemy import select, text
from sqlalchemy.orm import Session

from .database import Dataset, DatasetVersion, ImportJob, Namespace, Tenant


class IdempotencyConflict(ValueError):
    pass


@dataclass(frozen=True)
class CreatedDataset:
    dataset: Dataset
    version: DatasetVersion
    job: ImportJob
    created: bool


def create_dataset(
    session: Session,
    *,
    tenant_id: str,
    namespace: str,
    idempotency_key: str,
    original_file_name: str,
    content: bytes,
) -> CreatedDataset:
    digest = sha256(content).hexdigest()
    session.execute(
        text("SELECT pg_advisory_xact_lock(hashtextextended(:key, 0))"),
        {"key": f"dataset-scope:{tenant_id}:{namespace}"},
    )
    if session.get(Tenant, tenant_id) is None:
        session.add(Tenant(id=tenant_id))
    namespace_row = session.scalar(
        select(Namespace).where(Namespace.tenant_id == tenant_id, Namespace.name == namespace)
    )
    if namespace_row is None:
        session.add(Namespace(tenant_id=tenant_id, name=namespace))
    existing = session.scalar(
        select(Dataset).where(
            Dataset.tenant_id == tenant_id,
            Dataset.namespace == namespace,
            Dataset.idempotency_key == idempotency_key,
        )
    )
    if existing is not None:
        if existing.content_sha256 != digest:
            raise IdempotencyConflict("idempotency_payload_mismatch")
        version = session.scalar(
            select(DatasetVersion)
            .where(DatasetVersion.dataset_id == existing.id)
            .order_by(DatasetVersion.version_number.desc())
            .limit(1)
        )
        assert version is not None
        job = session.scalar(select(ImportJob).where(ImportJob.version_id == version.id))
        assert job is not None
        return CreatedDataset(existing, version, job, False)

    dataset = Dataset(
        id=uuid4(),
        tenant_id=tenant_id,
        namespace=namespace,
        idempotency_key=idempotency_key,
        content_sha256=digest,
        original_file_name=original_file_name,
    )
    version = DatasetVersion(id=uuid4(), dataset=dataset, version_number=1)
    session.add_all([dataset, version])
    session.flush()
    job = ImportJob(id=uuid4(), tenant_id=tenant_id, dataset_id=dataset.id, version_id=version.id)
    session.add(job)
    session.commit()
    return CreatedDataset(dataset, version, job, True)
