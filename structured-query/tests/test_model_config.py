from types import SimpleNamespace

from structured_query import model_config


def test_runtime_models_are_loaded_from_weknora_and_cached_per_tenant(monkeypatch):
    calls = []

    def fake_get(url, **kwargs):
        calls.append((url, kwargs))
        return SimpleNamespace(
            raise_for_status=lambda: None,
            json=lambda: {
                "chat": {"id": "chat-id", "name": "offline-chat", "base_url": "http://chat/v1", "api_key": "chat-key"},
                "embedding": {"id": "embed-id", "name": "offline-embed", "base_url": "http://embed/v1", "api_key": "embed-key", "dimension": 1024},
            },
        )

    monkeypatch.setattr(model_config.httpx, "get", fake_get)
    first = model_config.get_runtime_model_config("tenant-a")
    second = model_config.get_runtime_model_config("tenant-a")
    assert first is second
    assert first.chat.name == "offline-chat"
    assert first.embedding.dimension == 1024
    assert len(calls) == 1
    assert calls[0][1]["params"] == {"tenant_id": "tenant-a"}
    assert calls[0][1]["headers"] == {"X-Structured-Query-Key": "test-service-key"}
