# Kubernetes

Deploy the gateway and PostgreSQL to any Kubernetes cluster using the Kustomize manifests in `k8s/`. The manifests include a bundled PostgreSQL StatefulSet for convenience — in production, you'll likely replace it with a managed database.

## Prerequisites

- A Kubernetes cluster (v1.25+)
- `kubectl` configured to talk to your cluster
- An LLM API key (OpenAI, Anthropic, or Google)

## Step 1: Build and Push the Gateway Image

The K8s deployment needs the gateway image in a registry your cluster can pull from.

```bash
git clone https://github.com/superset-studio/majordomo-gateway.git
cd majordomo-gateway

# Build the image
docker build -t majordomo-gateway .

# Tag for your registry
docker tag majordomo-gateway your-registry.example.com/majordomo-gateway:latest

# Push to registry
docker push your-registry.example.com/majordomo-gateway:latest
```

Then update `k8s/kustomization.yaml` to point to your registry:

```yaml
images:
  - name: majordomo-gateway
    newName: your-registry.example.com/majordomo-gateway
    newTag: latest
```

## Step 2: Configure Secrets

Edit `k8s/secret.yaml` and replace the placeholder passwords. Values must be base64-encoded:

```bash
# Generate a base64-encoded password
echo -n 'your-strong-postgres-password' | base64
```

```yaml
# k8s/secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: majordomo-gateway-secrets
type: Opaque
data:
  MAJORDOMO_STORAGE_POSTGRES_PASSWORD: <base64-encoded-password>
  MAJORDOMO_S3_ACCESS_KEY_ID: <base64-encoded-key>          # optional
  MAJORDOMO_S3_SECRET_ACCESS_KEY: <base64-encoded-secret>    # optional
```

!!! warning "Don't commit real secrets"
    The `k8s/secret.yaml` file contains placeholder values. For production, use [sealed-secrets](https://github.com/bitnami-labs/sealed-secrets) or [external-secrets-operator](https://external-secrets.io/) instead of committing secrets to version control.

## Step 3: Deploy

```bash
kubectl apply -k k8s/
```

This creates the following resources in the `majordomo` namespace:

| Resource | Name | Notes |
|----------|------|-------|
| Namespace | `majordomo` | All resources live here |
| ConfigMap | `majordomo-gateway-config` | Contains `majordomo.yaml` |
| Secret | `majordomo-gateway-secrets` | DB password, optional S3 credentials |
| StatefulSet | `majordomo-postgres` | Single-replica Postgres 16 with 10Gi PVC |
| Deployment | `majordomo-gateway` | 2 replicas with health checks |
| Service | `majordomo-postgres` | ClusterIP, port 5432 |
| Service | `majordomo-gateway` | ClusterIP, port 7680 |

Wait for all pods to be ready:

```bash
kubectl -n majordomo get pods -w
```

You should see something like:

```
NAME                                  READY   STATUS    RESTARTS   AGE
majordomo-gateway-5d4f8b7c9-abc12     1/1     Running   0          30s
majordomo-gateway-5d4f8b7c9-def34     1/1     Running   0          30s
majordomo-postgres-0                  1/1     Running   0          45s
```

## Step 4: Apply the Database Schema

The bundled Postgres StatefulSet starts with an empty database. Apply the schema:

```bash
kubectl -n majordomo cp schema.sql majordomo-postgres-0:/tmp/schema.sql
kubectl -n majordomo exec majordomo-postgres-0 -- \
  psql -U majordomo -d majordomo -f /tmp/schema.sql
```

Verify the tables were created:

```bash
kubectl -n majordomo exec majordomo-postgres-0 -- \
  psql -U majordomo -d majordomo -c "\dt"
```

You should see `api_keys`, `llm_requests`, and `llm_requests_metadata_keys`.

!!! note "Using a managed database?"
    If you're using RDS, Cloud SQL, or another managed PostgreSQL, apply `schema.sql` using `psql` from your local machine or a bastion host instead. Then update the ConfigMap in `k8s/configmap.yaml` to point `storage.postgres.host` at your managed instance, and remove `k8s/postgres.yaml` from `k8s/kustomization.yaml`.

## Step 5: Verify the Gateway is Running

Port-forward to the gateway service:

```bash
kubectl -n majordomo port-forward svc/majordomo-gateway 7680:7680
```

In another terminal:

```bash
# Liveness
curl http://localhost:7680/health
# ok

# Readiness (checks DB connectivity)
curl http://localhost:7680/readyz
# {"status":"ok"}
```

If `/readyz` fails, check the gateway logs:

```bash
kubectl -n majordomo logs -l app=majordomo-gateway --tail=50
```

## Step 6: Create a Majordomo API Key

```bash
kubectl -n majordomo exec -it deploy/majordomo-gateway -- \
  /app/majordomo-proxy keys create --name "My Team"
```

Output:

```
API Key created successfully!

  ID:   a1b2c3d4-e5f6-7890-abcd-ef1234567890
  Name: My Team
  Key:  mdm_sk_abc123def456...

⚠️  Save this key now - it cannot be retrieved later.
```

## Step 7: Send a Test Request

With the port-forward still running:

```bash
curl -X POST http://localhost:7680/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Majordomo-Key: mdm_sk_your_key_here" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "Say hello"}]
  }'
```

You should get a normal OpenAI response back.

## Step 8: Verify Logging

```bash
kubectl -n majordomo exec majordomo-postgres-0 -- \
  psql -U majordomo -d majordomo -c \
  "SELECT model, input_tokens, output_tokens, total_cost FROM llm_requests ORDER BY created_at DESC LIMIT 1;"
```

You should see one row with the model name, token counts, and calculated cost.

## Exposing the Gateway

The default Service type is `ClusterIP`, so the gateway is only reachable from within the cluster. To expose it:

**Option A: LoadBalancer** — edit `k8s/service.yaml`:

```yaml
spec:
  type: LoadBalancer
```

**Option B: Ingress** — create an Ingress resource pointing to the `majordomo-gateway` service on port 7680.

**Option C: Internal only** — keep `ClusterIP` and have your applications connect via `majordomo-gateway.majordomo.svc.cluster.local:7680`.

## Production Considerations

- **Database**: The included StatefulSet is suitable for evaluation. For production, use a managed PostgreSQL (RDS, Cloud SQL, AlloyDB) and remove `k8s/postgres.yaml` from `kustomization.yaml`.
- **Replicas and resources**: Adjust `replicas`, `resources.requests`, and `resources.limits` in `k8s/deployment.yaml` based on your traffic.
- **Schema migrations**: On upgrades, check the changelog for schema changes and apply them with `psql -f schema.sql` (the schema uses `IF NOT EXISTS` so it's safe to re-run, but new columns may require `ALTER TABLE`).

## Next Steps

- Distribute the Majordomo API key to your team (see the [Getting Started](../getting-started.md#integrating-with-your-application) guide for SDK integration examples)
- Enable [S3 body storage](../getting-started.md#s3-body-storage) for full request/response capture
