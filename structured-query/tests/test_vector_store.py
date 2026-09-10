from types import SimpleNamespace
from uuid import uuid4

from structured_query import vector_store


class FakeMilvus:
    def __init__(self) -> None:
        self.search_filter = ""

    def search(self, collection, *, data, filter, limit, output_fields):
        assert collection and data and limit == 5 and output_fields
        self.search_filter = filter
        return [[]]


def test_search_always_applies_scalar_scope_and_escapes_namespace(monkeypatch):
    fake = FakeMilvus()
    version = uuid4()
    monkeypatch.setattr(vector_store, "ensure_collection", lambda _tenant_id: None)
    monkeypatch.setattr(vector_store, "embed_texts", lambda _tenant_id, _texts: [[0.0] * 1024])
    monkeypatch.setattr(vector_store, "milvus_client", lambda: fake)
    monkeypatch.setattr(
        vector_store,
        "get_runtime_model_config",
        lambda _tenant_id: SimpleNamespace(
            embedding=SimpleNamespace(id="embedding-id", name="embedding", base_url="http://model", dimension=1024)
        ),
    )

    result = vector_store.search_profiles(
        tenant_id="tenant-a",
        namespace='研发" or tenant_id != "tenant-a',
        version_ids=[version],
        question="人数",
        kind="table",
        limit=5,
    )

    assert result == []
    assert 'tenant_id == "tenant-a"' in fake.search_filter
    assert f'version_id in ["{version}"]' in fake.search_filter
    assert 'kind == "table"' in fake.search_filter
    assert '\\" or tenant_id' in fake.search_filter


def test_collection_isolated_by_tenant_and_embedding_profile(monkeypatch):
    monkeypatch.setattr(vector_store, "get_settings", lambda: SimpleNamespace(milvus_collection_prefix="profiles"))
    monkeypatch.setattr(
        vector_store,
        "get_runtime_model_config",
        lambda tenant: SimpleNamespace(
            embedding=SimpleNamespace(id=f"embedding-{tenant}", name="embedding", base_url="http://model", dimension=1024)
        ),
    )
    assert vector_store.collection_name("tenant-a") != vector_store.collection_name("tenant-b")


def test_search_reuses_supplied_query_vector_without_embedding_call(monkeypatch):
    fake = FakeMilvus()
    monkeypatch.setattr(vector_store, "ensure_collection", lambda _tenant_id: None)
    monkeypatch.setattr(
        vector_store,
        "embed_texts",
        lambda *_args: (_ for _ in ()).throw(AssertionError("embedding must be reused")),
    )
    monkeypatch.setattr(vector_store, "milvus_client", lambda: fake)
    monkeypatch.setattr(vector_store, "collection_name", lambda _tenant_id: "profiles")

    result = vector_store.search_profiles(
        tenant_id="tenant-a", namespace="default", version_ids=[uuid4()],
        question="人数", kind="table", limit=5, query_vector=[0.25] * 8,
    )

    assert result == []


def test_truncate_utf8_respects_milvus_byte_limit():
    value = "中" * 4000

    truncated = vector_store._truncate_utf8(value, 8192)

    assert len(truncated.encode("utf-8")) <= 8192
    assert truncated == "中" * 2730


def test_index_documents_bounds_embedding_and_insert_batches(monkeypatch):
    batches = []
    inserts = []
    fake = SimpleNamespace(insert=lambda collection, rows: inserts.append((collection, len(rows))))
    monkeypatch.setattr(vector_store, "ensure_collection", lambda _tenant_id: None)
    monkeypatch.setattr(vector_store, "collection_name", lambda _tenant_id: "profiles")
    monkeypatch.setattr(vector_store, "milvus_client", lambda: fake)

    def embed(_tenant_id, texts):
        batches.append(len(texts))
        return [[0.0] for _ in texts]

    monkeypatch.setattr(vector_store, "embed_texts", embed)
    documents = [
        vector_store.ProfileDocument("tenant-a", "ns", uuid4(), "value", uuid4(), None, str(index))
        for index in range(1_001)
    ]

    vector_store.index_documents(documents)

    assert batches == [500, 500, 1]
    assert [size for _, size in inserts] == [500, 500, 1]
