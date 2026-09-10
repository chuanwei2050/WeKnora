from types import SimpleNamespace
from uuid import uuid4

from structured_query import reindex


def test_rebuild_replaces_each_active_version(monkeypatch):
    version = uuid4()
    dataset = SimpleNamespace(tenant_id="tenant", namespace="kb")
    version_row = SimpleNamespace(id=version)
    table = SimpleNamespace(id=uuid4(), sheet_name="人员", profile={"mschema": "schema"})
    column = SimpleNamespace(id=uuid4(), original_name="姓名", data_type="TEXT")

    class Scalars:
        def __init__(self, rows): self.rows = rows
        def __iter__(self): return iter(self.rows)
        def all(self): return self.rows

    class Session:
        def __enter__(self): return self
        def __exit__(self, *_): return None
        def execute(self, statement):
            text = str(statement)
            if "sq_column_values" in text: return Scalars([("张三", 1)])
            return Scalars([(dataset, version_row)])
        def scalars(self, statement):
            return Scalars([column] if "sq_columns" in str(statement) else [table])

    deleted, indexed = [], []
    monkeypatch.setattr(reindex, "session_factory", lambda: lambda: Session())
    monkeypatch.setattr(reindex, "delete_version", lambda tenant, version_id: deleted.append((tenant, version_id)))
    monkeypatch.setattr(reindex, "index_documents", lambda docs: indexed.extend(docs))
    result = reindex.rebuild_profile_index()
    assert result == {"versions": 1, "documents": 3}
    assert deleted == [("tenant", version)] and {item.kind for item in indexed} == {"table", "column", "value"}
