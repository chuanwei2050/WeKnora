def test_worker_registers_import_and_profile_tasks(configured_environment):
    from structured_query.worker import celery_app

    celery_app.loader.import_default_modules()

    assert "structured_query.import_dataset" in celery_app.tasks
    assert "structured_query.profile_datasource" in celery_app.tasks
