# Deployment

## Docker Compose

The fastest way to run the gateway — Docker Compose starts both the gateway and PostgreSQL with a single command.

### Prerequisites

- Docker and Docker Compose (v2)

### Setup

```bash
# 1. Configure credentials
cp .env.example .env
# Edit .env — set MAJORDOMO_STORAGE_POSTGRES_PASSWORD at minimum

# 2. Start the stack
make compose-up    # or: docker compose up --build -d
```

The gateway is available at `http://localhost:7680`. PostgreSQL is on port `5432`.

`schema.sql` is mounted into the Postgres container and applied automatically on first start (via `/docker-entrypoint-initdb.d/`). On subsequent starts the existing data volume is preserved.

### Verify

```bash
# Liveness
curl http://localhost:7680/health
# ok

# Readiness (checks DB connectivity)
curl http://localhost:7680/readyz
# {"status":"ok"}
```

### Create an API key

```bash
docker compose exec gateway /app/majordomo-proxy keys create --name "My Key"
```

### Stop

```bash
make compose-down  # or: docker compose down
```

Add `-v` to also remove the Postgres data volume:

```bash
docker compose down -v
```

---

## Docker (standalone)

Use this if you already have a PostgreSQL instance.

### Build the image

```bash
docker build -t majordomo-gateway .
```

### Run

```bash
docker run -p 7680:7680 \
  -e MAJORDOMO_STORAGE_POSTGRES_HOST=host.docker.internal \
  -e MAJORDOMO_STORAGE_POSTGRES_PORT=5432 \
  -e MAJORDOMO_STORAGE_POSTGRES_USER=majordomo \
  -e MAJORDOMO_STORAGE_POSTGRES_PASSWORD=secret \
  -e MAJORDOMO_STORAGE_POSTGRES_DATABASE=majordomo \
  majordomo-gateway
```

Make sure you've applied `schema.sql` to your database before starting.

---

## Health endpoints

The gateway exposes two health endpoints for orchestrators like Kubernetes, ECS, or Docker Compose.

| Endpoint | Purpose | Healthy | Unhealthy |
|----------|---------|---------|-----------|
| `GET /health` | **Liveness** — is the process running? | `200 ok` | — (process is dead) |
| `GET /readyz` | **Readiness** — can it serve traffic? | `200 {"status":"ok"}` | `503 {"status":"error","error":"..."}` |

### Kubernetes example

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 7680
  initialDelaySeconds: 5
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /readyz
    port: 7680
  initialDelaySeconds: 5
  periodSeconds: 10
```

`/readyz` pings PostgreSQL with a 3-second timeout. If the database is unreachable the gateway returns `503` and the orchestrator stops routing traffic to it until it recovers.
