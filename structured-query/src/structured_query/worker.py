from celery import Celery

from .config import get_settings


settings = get_settings()
celery_app = Celery(
    "structured_query",
    broker=settings.redis_url.get_secret_value(),  # type: ignore[union-attr]
    backend=settings.redis_url.get_secret_value(),  # type: ignore[union-attr]
    include=["structured_query.import_tasks", "structured_query.profile_tasks"],
)
celery_app.conf.update(
    task_serializer="json",
    result_serializer="json",
    accept_content=["json"],
    task_track_started=True,
    task_acks_late=True,
    worker_prefetch_multiplier=1,
)
