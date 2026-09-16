from dataclasses import dataclass
from hashlib import sha256
from uuid import UUID, uuid4

from sqlalchemy import select, text
from sqlalchemy.orm import Session, selectinload

from .database import Dataset, DatasetSourceType, DatasetVersion, ImportJob, Namespace, Tenant
from .vector_store import delete_version


class IdempotencyConflict(ValueError):
    pass


@dataclass(frozen=True)
class CreatedDataset:
    dataset: Dataset
    version: DatasetVersion
    job: ImportJob
    created: bool


def _drop_managed_physical_tables(session: Session, dataset: Dataset) -> None:
    if dataset.source_type != DatasetSourceType.MANAGED_FILE:
        return
    for version in dataset.versions:
        for table in version.tables:
            schema = table.physical_schema.replace('"', "")
            name = table.physical_name.replace('"', "")
            session.execute(text(f'DROP TABLE IF EXISTS "{schema}"."{name}"'))


def _purge_dataset(session: Session, dataset: Dataset) -> None:
    """Drop physical tables/vectors, then delete the dataset via DB CASCADE.

    Clearing ``active_version_id`` and calling ``session.delete(dataset)`` makes
    SQLAlchemy NULL ``DatasetVersion.dataset_id`` before the parent row is gone,
    which violates the NOT NULL FK. Delete children/parent with SQL instead so
    ``ON DELETE CASCADE`` can run.
    """
    _drop_managed_physical_tables(session, dataset)
    for version in list(dataset.versions):
        try:
            delete_version(dataset.tenant_id, version.id)
        except Exception:
            pass
    session.execute(
        text("UPDATE sq_datasets SET active_version_id = NULL WHERE id = :id"),
        {"id": dataset.id},
    )
    session.execute(text("DELETE FROM sq_datasets WHERE id = :id"), {"id": dataset.id})
    session.expire_all()


def delete_datasets_by_idempotency_prefix(
    session: Session,
    *,
    tenant_id: str,
    namespace: str,
    idempotency_prefix: str,
) -> int:
    """Delete sidecar datasets whose idempotency_key starts with prefix.

    File imports use keys like ``{knowledge_id}-{file_hash}`` and maintenance
    rebuilds append ``-{run_id}``, so prefix ``{knowledge_id}-`` clears both the
    current binding and orphaned rebuild copies.
    """
    prefix = (idempotency_prefix or "").strip()
    if not prefix:
        return 0
    session.execute(
        text("SELECT pg_advisory_xact_lock(hashtextextended(:key, 0))"),
        {"key": f"dataset-scope:{tenant_id}:{namespace}"},
    )
    datasets = list(
        session.scalars(
            select(Dataset)
            .options(
                selectinload(Dataset.versions).selectinload(DatasetVersion.tables),
            )
            .where(
                Dataset.tenant_id == tenant_id,
                Dataset.namespace == namespace,
                Dataset.idempotency_key.startswith(prefix),
            )
        ).unique()
    )
    deleted = 0
    for dataset in datasets:
        _purge_dataset(session, dataset)
        deleted += 1
    if deleted:
        session.commit()
    return deleted


def delete_dataset(session: Session, *, tenant_id: str, dataset_id: UUID) -> bool:
    dataset = session.scalar(
        select(Dataset)
        .options(selectinload(Dataset.versions).selectinload(DatasetVersion.tables))
        .where(Dataset.id == dataset_id, Dataset.tenant_id == tenant_id)
    )
    if dataset is None:
        return False
    session.execute(
        text("SELECT pg_advisory_xact_lock(hashtextextended(:key, 0))"),
        {"key": f"dataset-scope:{tenant_id}:{dataset.namespace}"},
    )
    _purge_dataset(session, dataset)
    session.commit()
    return True


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
