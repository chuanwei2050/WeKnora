from functools import lru_cache
from typing import BinaryIO

import boto3
from botocore.client import BaseClient
from botocore.exceptions import ClientError

from .config import get_settings


@lru_cache
def s3_client() -> BaseClient:
    settings = get_settings()
    return boto3.client(
        "s3",
        endpoint_url=settings.s3_endpoint,
        aws_access_key_id=settings.s3_access_key.get_secret_value(),
        aws_secret_access_key=settings.s3_secret_key.get_secret_value(),
        region_name=settings.s3_region,
    )


def ensure_bucket() -> None:
    settings = get_settings()
    client = s3_client()
    try:
        client.head_bucket(Bucket=settings.s3_bucket)
    except ClientError:
        client.create_bucket(Bucket=settings.s3_bucket)


def upload_stream(key: str, stream: BinaryIO) -> None:
    ensure_bucket()
    s3_client().upload_fileobj(stream, get_settings().s3_bucket, key)


def download_bytes(key: str) -> bytes:
    response = s3_client().get_object(Bucket=get_settings().s3_bucket, Key=key)
    return response["Body"].read()


def delete_object(key: str) -> None:
    s3_client().delete_object(Bucket=get_settings().s3_bucket, Key=key)
