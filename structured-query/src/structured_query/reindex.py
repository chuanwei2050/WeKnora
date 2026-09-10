from __future__ import annotations

from uuid import UUID

from sqlalchemy import select

from .database import ColumnValue, DataColumn, DataTable, Dataset, DatasetVersion, session_factory
from .vector_store import ProfileDocument, delete_version, index_documents


def rebuild_profile_index(version_id: UUID | None = None) -> dict[str, int]:
    with session_factory()() as session:
        statement = (
            select(Dataset, DatasetVersion)
            .join(DatasetVersion, DatasetVersion.dataset_id == Dataset.id)
            .where(Dataset.active_version_id == DatasetVersion.id)
        )
        if version_id is not None:
            statement = statement.where(DatasetVersion.id == version_id)
        versions = session.execute(statement).all()
        document_count = 0
        for dataset, version in versions:
            documents: list[ProfileDocument] = []
            tables = list(session.scalars(select(DataTable).where(DataTable.version_id == version.id)))
            for table in tables:
                documents.append(ProfileDocument(dataset.tenant_id, dataset.namespace, version.id, "table", table.id, None, f"{table.sheet_name}\n{table.profile.get('mschema', '')}"))
                columns = list(session.scalars(select(DataColumn).where(DataColumn.table_id == table.id)))
                for column in columns:
                    documents.append(ProfileDocument(dataset.tenant_id, dataset.namespace, version.id, "column", table.id, column.id, f"{table.sheet_name} {column.original_name} {column.data_type}"))
                    # ColumnValue rows already represent the bounded, policy-approved
                    # persisted set. Rebuild all of them so versions imported before
                    # the indexed marker was introduced remain recoverable.
                    values = session.execute(
                        select(ColumnValue.value, ColumnValue.frequency).where(ColumnValue.column_id == column.id)
                    ).all()
                    documents.extend(ProfileDocument(dataset.tenant_id, dataset.namespace, version.id, "value", table.id, column.id, f"{column.original_name}: {value}", frequency) for value, frequency in values)
            delete_version(dataset.tenant_id, version.id)
            index_documents(documents)
            document_count += len(documents)
        return {"versions": len(versions), "documents": document_count}
