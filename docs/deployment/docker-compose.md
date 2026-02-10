# Docker Compose

The fastest way to run the gateway — Docker Compose starts both the gateway and PostgreSQL with a single command.

## Prerequisites

- Docker and Docker Compose (v2)

## Setup

```bash
# 1. Configure credentials
cp .env.example .env
# Edit .env — set MAJORDOMO_STORAGE_POSTGRES_PASSWORD at minimum

# 2. Start the stack
make compose-up    # or: docker compose up --build -d
```

The gateway is available at `http://localhost:7680`. PostgreSQL is on port `5432`.

`schema.sql` is mounted into the Postgres container and applied automatically on first start (via `/docker-entrypoint-initdb.d/`). On subsequent starts the existing data volume is preserved.

## Verify

```bash
# Liveness
curl http://localhost:7680/health
# ok

# Readiness (checks DB connectivity)
curl http://localhost:7680/readyz
# {"status":"ok"}
```

## Create an API key

```bash
docker compose exec gateway /app/majordomo-proxy keys create --name "My Key"
```

## Stop

```bash
make compose-down  # or: docker compose down
```

Add `-v` to also remove the Postgres data volume:

```bash
docker compose down -v
```
