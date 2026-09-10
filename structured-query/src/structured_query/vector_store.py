from dataclasses import dataclass
from functools import lru_cache
from hashlib import sha256
import json
from uuid import UUID, uuid4

from pymilvus import DataType, MilvusClient

from .config import get_settings
from .embeddings import embed_texts
from .model_config import get_runtime_model_config


PROFILE_TEXT_MAX_BYTES = 8192


@dataclass(frozen=True)
class ProfileDocument:
    tenant_id: str
    namespace: str
    version_id: UUID
    kind: str
    table_id: UUID
    column_id: UUID | None
    text: str
    frequency: int = 0


def collection_name(tenant_id: str) -> str:
    embedding = get_runtime_model_config(tenant_id).embedding
    fingerprint = sha256(
        f"{tenant_id}\0{embedding.id}\0{embedding.name}\0{embedding.base_url}\0{embedding.dimension}".encode()
    ).hexdigest()[:16]
    prefix = get_settings().milvus_collection_prefix[:180]
    return f"{prefix}_profiles_v2_{fingerprint}"


@lru_cache
def milvus_client() -> MilvusClient:
    settings = get_settings()
    kwargs = {"uri": settings.milvus_uri, "db_name": settings.milvus_database}
    token = settings.milvus_token.get_secret_value()
    if token:
        kwargs["token"] = token
    return MilvusClient(**kwargs)


def ensure_collection(tenant_id: str) -> None:
    client = milvus_client()
    name = collection_name(tenant_id)
    if client.has_collection(name):
        return
    schema = MilvusClient.create_schema(auto_id=False, enable_dynamic_field=False)
    schema.add_field("id", DataType.VARCHAR, is_primary=True, max_length=64)
    schema.add_field("vector", DataType.FLOAT_VECTOR, dim=get_runtime_model_config(tenant_id).embedding.dimension)
    schema.add_field("tenant_id", DataType.VARCHAR, max_length=128)
    schema.add_field("namespace", DataType.VARCHAR, max_length=128)
    schema.add_field("version_id", DataType.VARCHAR, max_length=36)
    schema.add_field("kind", DataType.VARCHAR, max_length=16)
    schema.add_field("table_id", DataType.VARCHAR, max_length=36)
    schema.add_field("column_id", DataType.VARCHAR, max_length=36)
    schema.add_field("text", DataType.VARCHAR, max_length=8192)
    schema.add_field("frequency", DataType.INT64)
    index = MilvusClient.prepare_index_params()
    index.add_index("vector", index_type="AUTOINDEX", metric_type="COSINE")
    index.add_index("tenant_id", index_type="INVERTED")
    index.add_index("namespace", index_type="INVERTED")
    index.add_index("version_id", index_type="INVERTED")
    index.add_index("kind", index_type="INVERTED")
    client.create_collection(name, schema=schema, index_params=index)


def index_documents(documents: list[ProfileDocument]) -> None:
    if not documents:
        return
    ensure_collection(documents[0].tenant_id)
    if any(document.tenant_id != documents[0].tenant_id for document in documents):
        raise ValueError("mixed_tenant_profile_batch")
    tenant_id = documents[0].tenant_id
    for start in range(0, len(documents), 500):
        batch = documents[start : start + 500]
        texts = [_truncate_utf8(document.text, PROFILE_TEXT_MAX_BYTES) for document in batch]
        vectors = embed_texts(tenant_id, texts)
        rows = [
            {
                "id": uuid4().hex,
                "vector": vector,
                "tenant_id": document.tenant_id,
                "namespace": document.namespace,
                "version_id": str(document.version_id),
                "kind": document.kind,
                "table_id": str(document.table_id),
                "column_id": str(document.column_id or ""),
                "text": text,
                "frequency": document.frequency,
            }
            for document, text, vector in zip(batch, texts, vectors, strict=True)
        ]
        milvus_client().insert(collection_name(tenant_id), rows)


def _truncate_utf8(value: str, max_bytes: int) -> str:
    """Fit text into a Milvus VARCHAR limit without splitting a UTF-8 code point."""
    encoded = value.encode("utf-8")
    if len(encoded) <= max_bytes:
        return value
    return encoded[:max_bytes].decode("utf-8", errors="ignore")


def delete_version(tenant_id: str, version_id: UUID) -> None:
    name = collection_name(tenant_id)
    if milvus_client().has_collection(name):
        milvus_client().delete(
            name, filter=f"version_id == {_milvus_string_literal(str(version_id))}"
        )


def _milvus_string_literal(value: str) -> str:
    return json.dumps(value, ensure_ascii=False)


def search_profiles(
    *, tenant_id: str, namespace: str, version_ids: list[UUID], question: str, kind: str, limit: int,
    query_vector: list[float] | None = None,
) -> list[dict]:
    if not version_ids:
        return []
    ensure_collection(tenant_id)
    vector = query_vector if query_vector is not None else embed_texts(tenant_id, [question])[0]
    versions = ",".join(_milvus_string_literal(str(version_id)) for version_id in version_ids)
    expression = (
        f"tenant_id == {_milvus_string_literal(tenant_id)} "
        f"and namespace == {_milvus_string_literal(namespace)} "
        f"and version_id in [{versions}] and kind == {_milvus_string_literal(kind)}"
    )
    result = milvus_client().search(
        collection_name(tenant_id),
        data=[vector],
        filter=expression,
        limit=limit,
        output_fields=["version_id", "kind", "table_id", "column_id", "text", "frequency"],
    )
    hits = []
    for hit in result[0]:
        entity = hit.get("entity", {})
        hits.append({**entity, "score": float(hit["distance"])})
    return hits
