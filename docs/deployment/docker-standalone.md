# Docker (Standalone)

Use this when you have an existing PostgreSQL instance (managed or self-hosted) and want to run just the gateway container.

## Prerequisites

- Docker
- A running PostgreSQL 14+ instance that you can connect to
- An LLM API key (OpenAI, Anthropic, or Google)

## Step 1: Create the Database and Apply the Schema

Connect to your PostgreSQL instance and create a database for Majordomo:

```sql
CREATE DATABASE majordomo;
CREATE USER majordomo WITH PASSWORD 'your-password';
GRANT ALL PRIVILEGES ON DATABASE majordomo TO majordomo;
```

Apply the schema. You can do this from any machine that has `psql` access:

```bash
# If you cloned the repo:
psql -h your-postgres-host -U majordomo -d majordomo -f schema.sql

# Or download just the schema file:
curl -sL https://raw.githubusercontent.com/superset-studio/majordomo-gateway/main/schema.sql \
  | psql -h your-postgres-host -U majordomo -d majordomo
```

Verify the tables were created:

```bash
psql -h your-postgres-host -U majordomo -d majordomo -c "\dt"
```

You should see `api_keys`, `llm_requests`, and `llm_requests_metadata_keys`.

## Step 2: Build the Docker Image

```bash
git clone https://github.com/superset-studio/majordomo-gateway.git
cd majordomo-gateway
docker build -t majordomo-gateway .
```

Or, if the image is published to a registry, pull it directly:

```bash
docker pull ghcr.io/superset-studio/majordomo-gateway:latest
```

## Step 3: Run the Gateway

```bash
docker run -d \
  --name majordomo-gateway \
  -p 7680:7680 \
  -e MAJORDOMO_STORAGE_POSTGRES_HOST=your-postgres-host \
  -e MAJORDOMO_STORAGE_POSTGRES_PORT=5432 \
  -e MAJORDOMO_STORAGE_POSTGRES_USER=majordomo \
  -e MAJORDOMO_STORAGE_POSTGRES_PASSWORD=your-password \
  -e MAJORDOMO_STORAGE_POSTGRES_DATABASE=majordomo \
  majordomo-gateway
```

!!! note "Connecting to host-local Postgres"
    If PostgreSQL is running on the Docker host (not in a container), use `host.docker.internal` as the hostname on macOS/Windows. On Linux, add `--network=host` or use the host's IP address.

## Step 4: Verify the Gateway is Running

```bash
# Liveness
curl http://localhost:7680/health
# ok

# Readiness (checks DB connectivity)
curl http://localhost:7680/readyz
# {"status":"ok"}
```

If `/readyz` fails, check the logs:

```bash
docker logs majordomo-gateway
```

Common issues:

- **Connection refused** — Check that the Postgres host/port are reachable from inside the container.
- **Authentication failed** — Verify the username/password and that the user has access to the database.
- **SSL errors** — Add `-e MAJORDOMO_STORAGE_POSTGRES_SSLMODE=disable` for local/non-SSL Postgres, or `verify-full` for managed databases that require SSL.

## Step 5: Create a Majordomo API Key

```bash
docker exec majordomo-gateway /app/majordomo-proxy keys create --name "My Team"
```

Output:

```
API Key created successfully!

  ID:   a1b2c3d4-e5f6-7890-abcd-ef1234567890
  Name: My Team
  Key:  mdm_sk_abc123def456...

⚠️  Save this key now - it cannot be retrieved later.
```

## Step 6: Send a Test Request

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

## Step 7: Verify Logging

```bash
psql -h your-postgres-host -U majordomo -d majordomo -c \
  "SELECT model, input_tokens, output_tokens, total_cost FROM llm_requests ORDER BY created_at DESC LIMIT 1;"
```

You should see one row with the model name, token counts, and calculated cost.

## Optional: Enable S3 Body Storage

To capture full request/response bodies, add S3 configuration:

```bash
docker run -d \
  --name majordomo-gateway \
  -p 7680:7680 \
  -e MAJORDOMO_STORAGE_POSTGRES_HOST=your-postgres-host \
  -e MAJORDOMO_STORAGE_POSTGRES_PORT=5432 \
  -e MAJORDOMO_STORAGE_POSTGRES_USER=majordomo \
  -e MAJORDOMO_STORAGE_POSTGRES_PASSWORD=your-password \
  -e MAJORDOMO_STORAGE_POSTGRES_DATABASE=majordomo \
  -e MAJORDOMO_LOGGING_BODY_STORAGE=s3 \
  -e MAJORDOMO_S3_ENABLED=true \
  -e MAJORDOMO_S3_BUCKET=majordomo-logs \
  -e MAJORDOMO_S3_REGION=us-east-1 \
  -e MAJORDOMO_S3_ACCESS_KEY_ID=your-access-key \
  -e MAJORDOMO_S3_SECRET_ACCESS_KEY=your-secret-key \
  majordomo-gateway
```

For S3-compatible storage (MinIO, Cloudflare R2), add the endpoint:

```bash
  -e MAJORDOMO_S3_ENDPOINT=http://minio:9000
```

## Next Steps

- Distribute the Majordomo API key to your team (see the [Getting Started](../getting-started.md#integrating-with-your-application) guide for SDK integration examples)
- See [Health Endpoints](health-endpoints.md) for configuring health checks in your orchestrator
