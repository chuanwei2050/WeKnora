from dataclasses import dataclass

from fastapi import Depends, HTTPException, Security, status
from fastapi.security import APIKeyHeader

from .config import Settings, get_settings


@dataclass(frozen=True)
class Principal:
    tenant_id: str


_api_key = APIKeyHeader(name="X-API-Key", auto_error=False)
_tenant_id = APIKeyHeader(name="X-Tenant-ID", auto_error=False)


def authenticate(
    supplied: str | None = Security(_api_key),
    supplied_tenant: str | None = Security(_tenant_id),
    settings: Settings = Depends(get_settings),
) -> Principal:
    tenant = settings.api_keys.get(supplied or "")
    if tenant is None:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid_api_key")
    if tenant == "service":
        if supplied_tenant is None or not supplied_tenant.isdecimal() or int(supplied_tenant) <= 0:
            raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid_tenant")
        tenant = supplied_tenant
    elif supplied_tenant is not None and supplied_tenant != tenant:
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="tenant_mismatch")
    return Principal(tenant_id=tenant)
