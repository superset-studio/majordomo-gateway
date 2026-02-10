# Kubernetes

Deploy the gateway and PostgreSQL to any Kubernetes cluster using the raw manifests in `k8s/`.

## Prerequisites

- A Kubernetes cluster (v1.25+)
- `kubectl` configured to talk to your cluster
- `kustomize` (built into kubectl v1.14+)

## Quick start

```bash
# 1. Edit the secret — replace the placeholder base64 values
#    echo -n 'my-real-password' | base64
vi k8s/secret.yaml

# 2. Deploy everything into the "majordomo" namespace
kubectl apply -k k8s/

# 3. Verify pods are running
kubectl -n majordomo get pods

# 4. Apply the database schema (first deploy only)
kubectl -n majordomo exec -it majordomo-postgres-0 -- \
  psql -U majordomo -d majordomo -f /dev/stdin < schema.sql

# 5. Check health
kubectl -n majordomo port-forward svc/majordomo-gateway 7680:7680
curl http://localhost:7680/health    # ok
curl http://localhost:7680/readyz    # {"status":"ok"}
```

## What gets created

| Resource | Name | Notes |
|----------|------|-------|
| Namespace | `majordomo` | All resources live here |
| ConfigMap | `majordomo-gateway-config` | Contains `majordomo.yaml` |
| Secret | `majordomo-gateway-secrets` | DB password, optional S3 credentials |
| StatefulSet | `majordomo-postgres` | Single-replica Postgres 16 with 10Gi PVC |
| Deployment | `majordomo-gateway` | 2 replicas of the gateway |
| Service | `majordomo-postgres` | ClusterIP, port 5432 |
| Service | `majordomo-gateway` | ClusterIP, port 7680 |

## Customization

**Change the gateway image tag** — edit `k8s/kustomization.yaml`:

```yaml
images:
  - name: majordomo-gateway
    newName: ghcr.io/my-org/majordomo-gateway  # optional: change registry
    newTag: v1.2.3
```

**Adjust replicas or resources** — edit `k8s/deployment.yaml` directly, or create a Kustomize overlay.

**Expose the gateway externally** — change the Service type in `k8s/service.yaml` to `LoadBalancer`, or add an `Ingress` resource.

## Production notes

- **Secrets**: Do not commit real passwords to version control. Use [sealed-secrets](https://github.com/bitnami-labs/sealed-secrets) or [external-secrets-operator](https://external-secrets.io/) to manage secrets safely.
- **Database**: The included StatefulSet is suitable for development. For production, consider a managed PostgreSQL service (RDS, Cloud SQL, etc.) and remove the StatefulSet.
- **Schema migrations**: Apply `schema.sql` manually after the first deploy (see quick-start step 4). On subsequent deploys the existing data is preserved by the PersistentVolumeClaim.
