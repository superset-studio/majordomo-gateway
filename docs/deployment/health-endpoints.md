# Health endpoints

The gateway exposes two health endpoints for orchestrators like Kubernetes, ECS, or Docker Compose.

| Endpoint | Purpose | Healthy | Unhealthy |
|----------|---------|---------|-----------|
| `GET /health` | **Liveness** — is the process running? | `200 ok` | — (process is dead) |
| `GET /readyz` | **Readiness** — can it serve traffic? | `200 {"status":"ok"}` | `503 {"status":"error","error":"..."}` |

## Kubernetes example

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
