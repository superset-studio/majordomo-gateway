"""Fetch logged requests from PostgreSQL, optionally retrieving bodies from S3."""

import gzip
import json
import os
from dataclasses import dataclass

import asyncpg

from replay.parse import extract_prompts


@dataclass
class LoggedRequest:
    id: str
    user_prompt: str
    system_prompt: str | None
    original_response: str
    provider: str
    model: str
    cost: float
    latency_ms: int
    input_tokens: int
    output_tokens: int


async def fetch_requests(config: dict) -> list[LoggedRequest]:
    """Fetch logged requests from the database and parse them into LoggedRequests."""
    db_url = config.get("database", {}).get("url") or os.environ.get("DATABASE_URL")
    if not db_url:
        raise ValueError("Database URL not configured. Set database.url in config or DATABASE_URL env var.")

    source = config.get("source", {})
    filters = source.get("filters", {})
    model_filter = source.get("model")
    days = source.get("days", 30)
    limit = source.get("limit", 50)
    body_storage = config.get("body_storage", "postgres")

    # Build dynamic WHERE clause
    conditions = []
    params: list = []
    param_idx = 1

    for key, value in filters.items():
        conditions.append(f"raw_metadata->>'{key}' = ${param_idx}")
        params.append(str(value))
        param_idx += 1

    # Model filter (optional)
    conditions.append(f"(${param_idx}::varchar IS NULL OR model = ${param_idx})")
    params.append(model_filter)
    param_idx += 1

    # Time filter
    conditions.append(f"requested_at >= now() - make_interval(days => ${param_idx})")
    params.append(days)
    param_idx += 1

    # Must have bodies available
    conditions.append("(request_body IS NOT NULL OR body_s3_key IS NOT NULL)")

    # Limit
    limit_clause = f"LIMIT ${param_idx}"
    params.append(limit)

    where_clause = " AND ".join(conditions)

    query = f"""
        SELECT id, request_body, response_body, provider, model,
               input_tokens, output_tokens, total_cost, response_time_ms,
               raw_metadata, body_s3_key
        FROM llm_requests
        WHERE {where_clause}
        ORDER BY requested_at DESC
        {limit_clause}
    """

    conn = await asyncpg.connect(db_url)
    try:
        rows = await conn.fetch(query, *params)
    finally:
        await conn.close()

    # Load S3 client lazily if needed
    s3_client = None
    if body_storage == "s3":
        import boto3

        s3_config = config.get("s3", {})
        s3_client = boto3.client("s3", region_name=s3_config.get("region", "us-east-1"))
        s3_bucket = s3_config["bucket"]

    results: list[LoggedRequest] = []
    for row in rows:
        request_body = row["request_body"]
        response_body = row["response_body"]

        if body_storage == "s3" and row["body_s3_key"]:
            request_body, response_body = _fetch_bodies_from_s3(
                s3_client, s3_bucket, row["body_s3_key"]
            )

        if not request_body or not response_body:
            continue

        try:
            user_prompt, system_prompt, original_response = extract_prompts(
                request_body, response_body, row["provider"]
            )
        except (json.JSONDecodeError, KeyError, IndexError):
            continue

        if not user_prompt:
            continue

        results.append(
            LoggedRequest(
                id=str(row["id"]),
                user_prompt=user_prompt,
                system_prompt=system_prompt,
                original_response=original_response,
                provider=row["provider"],
                model=row["model"],
                cost=float(row["total_cost"] or 0),
                latency_ms=int(row["response_time_ms"] or 0),
                input_tokens=int(row["input_tokens"] or 0),
                output_tokens=int(row["output_tokens"] or 0),
            )
        )

    return results


def _fetch_bodies_from_s3(s3_client, bucket: str, key: str) -> tuple[str, str]:
    """Download and decompress request/response bodies from S3."""
    obj = s3_client.get_object(Bucket=bucket, Key=key)
    compressed = obj["Body"].read()
    decompressed = gzip.decompress(compressed)
    doc = json.loads(decompressed)

    request_body = json.dumps(doc["request"]["body"])
    response_body = json.dumps(doc["response"]["body"])
    return request_body, response_body
