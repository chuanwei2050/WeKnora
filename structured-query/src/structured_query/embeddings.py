from openai import OpenAI

from .config import get_settings
from .model_config import get_runtime_model_config


def embed_texts(tenant_id: str, texts: list[str]) -> list[list[float]]:
    if not texts:
        return []
    settings = get_settings()
    model = get_runtime_model_config(tenant_id).embedding
    client = OpenAI(
        base_url=model.base_url,
        api_key=model.api_key or "not-needed",
        timeout=settings.model_request_timeout_seconds,
        max_retries=0,
    )
    vectors: list[list[float]] = []
    for start in range(0, len(texts), settings.embedding_batch_size):
        response = client.embeddings.create(model=model.name, input=texts[start : start + settings.embedding_batch_size])
        vectors.extend(item.embedding for item in sorted(response.data, key=lambda item: item.index))
    if any(len(vector) != model.dimension for vector in vectors):
        raise ValueError("embedding_dimension_mismatch")
    return vectors
