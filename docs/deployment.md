# Deployment

Choose a deployment method based on your environment:

| Method | Best for |
|--------|----------|
| [Docker Compose](deployment/docker-compose.md) | Local development, quick evaluation |
| [Kubernetes](deployment/kubernetes.md) | Production clusters, team environments |
| [Docker (standalone)](deployment/docker-standalone.md) | Existing PostgreSQL, custom setups |

All methods require PostgreSQL. Docker Compose and Kubernetes manifests include a bundled Postgres instance. The standalone Docker option expects you to bring your own.

See [Health Endpoints](deployment/health-endpoints.md) for liveness and readiness probe details used by orchestrators.
