from structured_query import task_lock


class FakeRedis:
    def __init__(self) -> None:
        self.values: dict[str, str] = {}
        self.releases: list[tuple[str, str]] = []

    def set(self, key: str, value: str, *, nx: bool, ex: int) -> bool:
        assert nx is True
        assert ex > 0
        if key in self.values:
            return False
        self.values[key] = value
        return True

    def eval(self, script: str, keys: int, key: str, owner: str) -> int:
        assert script and keys == 1
        self.releases.append((key, owner))
        if self.values.get(key) == owner:
            del self.values[key]
            return 1
        return 0


def test_import_job_lock_is_owned_and_released(monkeypatch):
    redis = FakeRedis()
    monkeypatch.setattr(task_lock, "redis_client", lambda: redis)
    with task_lock.import_job_lock("job-1") as acquired:
        assert acquired is True
        with task_lock.import_job_lock("job-1") as duplicate:
            assert duplicate is False
    assert redis.values == {}
    assert len(redis.releases) == 1
