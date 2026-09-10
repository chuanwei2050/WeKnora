from hashlib import sha256
from io import BytesIO
from uuid import UUID

from fastapi import Depends, FastAPI, File, Form, Header, HTTPException, Response, UploadFile, status
from prometheus_client import CONTENT_TYPE_LATEST, generate_latest

from .auth import Principal, authenticate
from .config import Settings, get_settings
from .contracts import DatabaseDatasetCreate, DatasetAccepted, ErrorDetail, HealthResponse, JobResponse, JobState, QueryRequest, QueryResponse
from .database import ImportJob, initialize_database, session_factory
from .datasource_registry import refresh_datasource_profile, register_datasource
from .datasets import IdempotencyConflict, create_dataset as persist_dataset
from .object_store import upload_stream
from .observability import metrics_middleware


def create_app(*, initialize: bool = True) -> FastAPI:
    app = FastAPI(title="WeKnora Structured Query", version="1.0.0")
    app.middleware("http")(metrics_middleware)

    if initialize:
        @app.on_event("startup")
        def startup() -> None:
            initialize_database()

    @app.get("/v1/health/live", response_model=HealthResponse, tags=["health"])
    def live() -> HealthResponse:
        return HealthResponse(status="ok")

    @app.get("/metrics", include_in_schema=False)
    def metrics() -> Response:
        return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)

    @app.get("/v1/health/ready", response_model=HealthResponse, tags=["health"])
    def ready(settings: Settings = Depends(get_settings)) -> HealthResponse:
        return HealthResponse(
            status="configured",
            dependencies={
                "postgres": "configured" if settings.postgres_dsn else "missing",
                "redis": "configured" if settings.redis_url else "missing",
                "milvus": "configured" if settings.milvus_uri else "missing",
                "model_config_api": "configured" if settings.weknora_base_url else "missing",
            },
        )

    @app.post(
        "/v1/datasets/files",
        response_model=DatasetAccepted,
        status_code=status.HTTP_202_ACCEPTED,
        tags=["datasets"],
    )
    async def create_dataset(
        namespace: str = Form(min_length=1, max_length=128),
        file: UploadFile = File(),
        idempotency_key: str = Header(alias="Idempotency-Key", min_length=8, max_length=128),
        principal: Principal = Depends(authenticate),
        settings: Settings = Depends(get_settings),
    ) -> DatasetAccepted:
        if file.size is not None and file.size > settings.max_upload_bytes:
            raise HTTPException(status_code=413, detail="upload_too_large")
        suffix = (file.filename or "").lower().rsplit(".", 1)[-1]
        if suffix not in {"csv", "xls", "xlsx"}:
            raise HTTPException(status_code=415, detail="unsupported_file_type")
        content = await file.read(settings.max_upload_bytes + 1)
        if len(content) > settings.max_upload_bytes:
            raise HTTPException(status_code=413, detail="upload_too_large")
        with session_factory()() as session:
            try:
                created = persist_dataset(
                    session,
                    tenant_id=principal.tenant_id,
                    namespace=namespace,
                    idempotency_key=idempotency_key,
                    original_file_name=file.filename or "upload",
                    content=content,
                )
            except IdempotencyConflict as error:
                raise HTTPException(status_code=409, detail=str(error)) from error
        should_dispatch = created.created or created.job.state in {"queued", "failed"}
        if should_dispatch:
            object_key = f"{principal.tenant_id}/{created.version.id}/{sha256(content).hexdigest()}-{file.filename or 'upload'}"
            try:
                upload_stream(object_key, BytesIO(content))
                from .import_tasks import import_dataset

                import_dataset.delay(str(created.job.id), object_key, file.filename or "upload")
            except Exception as error:
                raise HTTPException(status_code=503, detail="dataset_dispatch_failed") from error
        return DatasetAccepted(dataset_id=created.dataset.id, version_id=created.version.id, job_id=created.job.id)

    @app.post("/v1/datasets", response_model=DatasetAccepted, status_code=status.HTTP_202_ACCEPTED, tags=["datasets"])
    def create_database_dataset(
        request: DatabaseDatasetCreate,
        idempotency_key: str = Header(alias="Idempotency-Key", min_length=8, max_length=128),
        principal: Principal = Depends(authenticate),
    ) -> DatasetAccepted:
        with session_factory()() as session:
            try:
                dataset, version, job, created = register_datasource(
                    session, principal.tenant_id, request, idempotency_key
                )
            except ValueError as error:
                raise HTTPException(status_code=409, detail=str(error)) from error
        if created or job.state in {"queued", "failed"}:
            try:
                from .profile_tasks import profile_datasource
                profile_datasource.delay(str(job.id))
            except Exception as error:
                raise HTTPException(status_code=503, detail="profile_dispatch_failed") from error
        return DatasetAccepted(dataset_id=dataset.id, version_id=version.id, job_id=job.id)

    @app.post(
        "/v1/datasets/{dataset_id}/profile-refresh",
        response_model=DatasetAccepted,
        status_code=status.HTTP_202_ACCEPTED,
        tags=["datasets"],
    )
    def refresh_database_dataset(
        dataset_id: UUID,
        idempotency_key: str = Header(alias="Idempotency-Key", min_length=8, max_length=128),
        principal: Principal = Depends(authenticate),
    ) -> DatasetAccepted:
        with session_factory()() as session:
            try:
                dataset, version, job, created = refresh_datasource_profile(
                    session, principal.tenant_id, dataset_id, idempotency_key
                )
            except ValueError as error:
                raise HTTPException(status_code=404, detail=str(error)) from error
        if created or job.state in {"queued", "failed"}:
            try:
                from .profile_tasks import profile_datasource

                profile_datasource.delay(str(job.id))
            except Exception as error:
                raise HTTPException(status_code=503, detail="profile_dispatch_failed") from error
        return DatasetAccepted(dataset_id=dataset.id, version_id=version.id, job_id=job.id)

    @app.get("/v1/jobs/{job_id}", response_model=JobResponse, tags=["jobs"])
    def get_job(job_id: UUID, principal: Principal = Depends(authenticate)) -> JobResponse:
        with session_factory()() as session:
            job = session.get(ImportJob, job_id)
            if job is None or job.tenant_id != principal.tenant_id:
                raise HTTPException(status_code=404, detail="job_not_found")
            error = None
            if job.error_code:
                error = ErrorDetail(code=job.error_code, message=job.error_message or "", retryable=False)
            return JobResponse(
                job_id=job.id,
                state=JobState(job.state),
                progress=job.progress,
                created_at=job.created_at,
                updated_at=job.updated_at,
                error=error,
            )

    @app.post("/v1/query", response_model=QueryResponse, tags=["query"])
    def query(request: QueryRequest, _: Principal = Depends(authenticate)) -> QueryResponse:
        from .query_service import run_query

        try:
            return run_query(_.tenant_id, request)
        except ValueError as error:
            raise HTTPException(status_code=422, detail=str(error)) from error

    return app


app = create_app()
