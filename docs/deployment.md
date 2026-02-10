# Deployment

This section covers deploying the gateway for team or production use. Each guide takes you from zero to a running, verified deployment.

| Method | Best for | PostgreSQL |
|--------|----------|------------|
| [Docker Compose](deployment/docker-compose.md) | Quick evaluation, small teams | Included |
| [Docker (standalone)](deployment/docker-standalone.md) | Existing PostgreSQL, custom setups | Bring your own |
| [Kubernetes](deployment/kubernetes.md) | Production clusters, team environments | Included (or bring your own) |

!!! tip "New to Majordomo?"
    If you just want to run the gateway locally on your machine to try it out, start with the [Getting Started](getting-started.md) guide instead. These deployment guides are for platform engineers setting up shared infrastructure.

## Configuration Reference

The gateway is configured via `majordomo.yaml` or environment variables. Environment variables use the `MAJORDOMO_` prefix and override the config file.

### Required Settings

| Environment Variable | Config Path | Description |
|---------------------|-------------|-------------|
| `MAJORDOMO_STORAGE_POSTGRES_HOST` | `storage.postgres.host` | PostgreSQL hostname |
| `MAJORDOMO_STORAGE_POSTGRES_PORT` | `storage.postgres.port` | PostgreSQL port (default: 5432) |
| `MAJORDOMO_STORAGE_POSTGRES_USER` | `storage.postgres.user` | PostgreSQL user |
| `MAJORDOMO_STORAGE_POSTGRES_PASSWORD` | `storage.postgres.password` | PostgreSQL password |
| `MAJORDOMO_STORAGE_POSTGRES_DATABASE` | `storage.postgres.database` | PostgreSQL database name |

### Optional Settings

| Environment Variable | Config Path | Default | Description |
|---------------------|-------------|---------|-------------|
| `MAJORDOMO_STORAGE_POSTGRES_SSLMODE` | `storage.postgres.sslmode` | `require` | SSL mode (`disable`, `require`, `verify-full`) |
| `MAJORDOMO_SERVER_HOST` | `server.host` | `0.0.0.0` | Listen address |
| `MAJORDOMO_SERVER_PORT` | `server.port` | `7680` | Listen port |
| `MAJORDOMO_LOGGING_BODY_STORAGE` | `logging.body_storage` | `none` | Where to store request/response bodies (`none`, `postgres`, `s3`) |
| `MAJORDOMO_S3_ENABLED` | `s3.enabled` | `false` | Enable S3 body storage |
| `MAJORDOMO_S3_BUCKET` | `s3.bucket` | | S3 bucket name |
| `MAJORDOMO_S3_REGION` | `s3.region` | | AWS region |
| `MAJORDOMO_S3_ENDPOINT` | `s3.endpoint` | | Custom endpoint for S3-compatible storage (MinIO, R2) |
| `MAJORDOMO_S3_ACCESS_KEY_ID` | | | AWS access key (or uses default credential chain) |
| `MAJORDOMO_S3_SECRET_ACCESS_KEY` | | | AWS secret key |

## Health Endpoints

All deployment methods should configure health checks using these endpoints:

| Endpoint | Purpose | Healthy | Unhealthy |
|----------|---------|---------|-----------|
| `GET /health` | **Liveness** — is the process running? | `200 ok` | Process is dead |
| `GET /readyz` | **Readiness** — can it serve traffic? | `200 {"status":"ok"}` | `503 {"status":"error","error":"..."}` |

`/readyz` pings PostgreSQL with a 3-second timeout. If the database is unreachable, the gateway returns `503` and the orchestrator stops routing traffic until it recovers.
