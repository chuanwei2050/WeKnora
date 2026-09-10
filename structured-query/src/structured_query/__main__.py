import os
import sys


def migrate_database() -> None:
    from alembic import command
    from alembic.config import Config

    command.upgrade(Config("alembic.ini"), "head")


def main() -> None:
    role = sys.argv[1] if len(sys.argv) > 1 else "api"
    if role == "migrate":
        migrate_database()
        return
    if role == "api":
        import uvicorn

        uvicorn.run("structured_query.api:app", host="0.0.0.0", port=8080)
        return
    if role == "worker":
        from .worker import celery_app

        arguments = ["worker", "--loglevel=INFO"]
        worker_pool = os.getenv("STRUCTURED_QUERY_WORKER_POOL", "").strip()
        if not worker_pool and sys.platform == "win32":
            worker_pool = "solo"
        if worker_pool:
            arguments.append(f"--pool={worker_pool}")
        celery_app.worker_main(arguments)
        return
    raise SystemExit(f"unknown role: {role}")


if __name__ == "__main__":
    main()
