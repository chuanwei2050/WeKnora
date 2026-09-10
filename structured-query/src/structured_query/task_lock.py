from contextlib import contextmanager
from functools import lru_cache
from secrets import token_urlsafe
from typing import Iterator

from redis import Redis

from .config import get_settings


_RELEASE_IF_OWNER = """
if redis.call('get', KEYS[1]) == ARGV[1] then
  return redis.call('del', KEYS[1])
end
return 0
"""


@lru_cache
def redis_client() -> Redis:
    return Redis.from_url(get_settings().redis_url.get_secret_value(), decode_responses=True)  # type: ignore[union-attr]


@contextmanager
def import_job_lock(job_id: str) -> Iterator[bool]:
    key = f"structured-query:import-lock:{job_id}"
    owner = token_urlsafe(24)
    client = redis_client()
    acquired = bool(client.set(key, owner, nx=True, ex=get_settings().import_lock_seconds))
    try:
        yield acquired
    finally:
        if acquired:
            client.eval(_RELEASE_IF_OWNER, 1, key, owner)
