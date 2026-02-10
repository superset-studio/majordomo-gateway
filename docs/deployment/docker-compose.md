# Docker Compose

Docker Compose starts both the gateway and PostgreSQL with a single command. The schema is applied automatically on first start.

## Prerequisites

- Docker and Docker Compose v2
- An LLM API key (OpenAI, Anthropic, or Google)

## Step 1: Clone and Configure

```bash
git clone https://github.com/superset-studio/majordomo-gateway.git
cd majordomo-gateway
```

Create an `.env` file from the example:

```bash
cp .env.example .env
```

Edit `.env` and set a Postgres password (the only required change):

```bash
# .env
MAJORDOMO_STORAGE_POSTGRES_USER=majordomo
MAJORDOMO_STORAGE_POSTGRES_PASSWORD=pick-a-strong-password   # <-- change this
MAJORDOMO_STORAGE_POSTGRES_DATABASE=majordomo
```

## Step 2: Start the Stack

```bash
docker compose up --build -d
```

This starts two containers:

- **postgres** — PostgreSQL 16 on port 5432, with `schema.sql` applied automatically via `/docker-entrypoint-initdb.d/`
- **gateway** — Majordomo Gateway on port 7680, waits for Postgres to be healthy before starting

## Step 3: Verify the Gateway is Running

```bash
# Liveness — is the process up?
curl http://localhost:7680/health
# ok

# Readiness — is the database connected?
curl http://localhost:7680/readyz
# {"status":"ok"}
```

If `/readyz` returns an error, check the gateway logs:

```bash
docker compose logs gateway
```

## Step 4: Create a Majordomo API Key

```bash
docker compose exec gateway /app/majordomo-proxy keys create --name "My Team"
```

Output:

```
API Key created successfully!

  ID:   a1b2c3d4-e5f6-7890-abcd-ef1234567890
  Name: My Team
  Key:  mdm_sk_abc123def456...

⚠️  Save this key now - it cannot be retrieved later.
```

Save this key — you and your team will pass it as the `X-Majordomo-Key` header.

## Step 5: Send a Test Request

Send a request through the gateway to verify end-to-end operation:

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

You should get a normal OpenAI response back. The gateway proxied the request and logged the usage.

## Step 6: Verify Logging

Confirm the request was logged to PostgreSQL:

```bash
docker compose exec postgres psql -U majordomo -d majordomo -c \
  "SELECT model, input_tokens, output_tokens, total_cost FROM llm_requests ORDER BY created_at DESC LIMIT 1;"
```

You should see one row with the model name, token counts, and calculated cost.

## Stopping and Restarting

```bash
# Stop (preserves data)
docker compose down

# Stop and delete the database volume
docker compose down -v

# Restart
docker compose up -d
```

## Next Steps

- Distribute the Majordomo API key to your team (see the [Getting Started](../getting-started.md#integrating-with-your-application) guide for SDK integration examples)
- Enable [S3 body storage](../getting-started.md#s3-body-storage) for full request/response capture
- For production use, consider the [Kubernetes](kubernetes.md) deployment or the [standalone Docker](docker-standalone.md) deployment with a managed PostgreSQL
