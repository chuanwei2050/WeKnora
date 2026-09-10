from datetime import datetime, timezone
from uuid import UUID

from sqlalchemy import select
from sqlalchemy.orm import Session

from .database import Dataset, DatasetVersion, VersionState


def activate_version(session: Session, dataset_id: UUID, version_id: UUID) -> None:
    dataset = session.scalar(select(Dataset).where(Dataset.id == dataset_id).with_for_update())
    version = session.scalar(
        select(DatasetVersion)
        .where(DatasetVersion.id == version_id, DatasetVersion.dataset_id == dataset_id)
        .with_for_update()
    )
    if dataset is None or version is None:
        raise ValueError("dataset_version_not_found")
    if version.state not in {VersionState.STAGING, VersionState.FAILED}:
        raise ValueError("dataset_version_not_staging")
    newer_pending_or_active = session.scalar(
        select(DatasetVersion.id)
        .where(
            DatasetVersion.dataset_id == dataset_id,
            DatasetVersion.version_number > version.version_number,
            DatasetVersion.state.in_([VersionState.STAGING, VersionState.ACTIVE]),
        )
        .limit(1)
    )
    if newer_pending_or_active is not None:
        version.state = VersionState.SUPERSEDED
        session.flush()
        return
    if dataset.active_version_id is not None and dataset.active_version_id != version.id:
        previous = session.get(DatasetVersion, dataset.active_version_id)
        if previous is not None:
            previous.state = VersionState.SUPERSEDED
    version.state = VersionState.ACTIVE
    version.activated_at = datetime.now(timezone.utc)
    dataset.active_version_id = version.id
    session.flush()
