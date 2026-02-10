# Getting Started with Majordomo Gateway

This guide walks you through setting up Majordomo Gateway and integrating it into your development environment.

## What is Majordomo Gateway?

Majordomo Gateway sits between your application and LLM providers (OpenAI, Anthropic, Google Gemini). It proxies requests transparently while:

- Logging every request with token counts and costs
- Tracking usage by API key, user, feature, or any custom dimension
- Calculating costs using real-time pricing data

```
Your App  →  Majordomo Gateway  →  OpenAI / Anthropic / Gemini
                    ↓
              PostgreSQL
            (logs & metrics)
```

## Prerequisites

- **Go 1.25+** - [Download Go](https://go.dev/dl/)
- **PostgreSQL 14+** - [Download PostgreSQL](https://www.postgresql.org/download/)
- **An LLM API key** - From OpenAI, Anthropic, or Google

## Step 1: Build the Gateway

```bash
git clone https://github.com/superset-studio/majordomo-gateway.git
cd majordomo-gateway
make build
```

This creates the binary at `./bin/majordomo`.

## Step 2: Set Up PostgreSQL

Create a database for Majordomo:

```bash
# Connect to PostgreSQL
psql -U postgres

# Create the database
CREATE DATABASE majordomo;

# Exit psql
\q

# Apply the schema
psql -U postgres -d majordomo -f schema.sql
```

The schema creates three tables:
- `api_keys` - Your Majordomo API keys
- `llm_requests` - Request logs with token counts, costs, and metadata
- `llm_requests_metadata_keys` - Configuration for indexed metadata fields

## Step 3: Configure the Gateway

Create a configuration file at `majordomo.yaml`:

```yaml
server:
  host: "127.0.0.1"
  port: 7680

storage:
  postgres:
    host: localhost
    port: 5432
    user: postgres
    password: your_password
    database: majordomo
```

Alternatively, use environment variables:

```bash
export MAJORDOMO_STORAGE_POSTGRES_HOST=localhost
export MAJORDOMO_STORAGE_POSTGRES_PORT=5432
export MAJORDOMO_STORAGE_POSTGRES_USER=postgres
export MAJORDOMO_STORAGE_POSTGRES_PASSWORD=your_password
export MAJORDOMO_STORAGE_POSTGRES_DATABASE=majordomo
```

## Step 4: Create an API Key

Before starting the gateway, create an API key:

```bash
./bin/majordomo keys create --name "Development"
```

Output:

```
API Key created successfully!

  ID:   a1b2c3d4-e5f6-7890-abcd-ef1234567890
  Name: Development
  Key:  mdm_sk_abc123def456...

⚠️  Save this key now - it cannot be retrieved later.
```

**Important:** Store this key securely. The plaintext key is only shown once.

## Step 5: Start the Gateway

```bash
./bin/majordomo serve
```

Or use the Makefile:

```bash
make run
```

You should see:

```
Starting Majordomo Gateway on 127.0.0.1:7680
Connected to PostgreSQL
Pricing data loaded (X models)
```

## Step 6: Test with curl

Make a test request through the gateway:

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

The gateway proxies the request to OpenAI and logs the usage.

## Step 7: Verify Logging

Check that the request was logged:

```bash
psql -U postgres -d majordomo -c "SELECT model, input_tokens, output_tokens, total_cost FROM llm_requests ORDER BY created_at DESC LIMIT 1;"
```

You should see your request with token counts and calculated cost.

---

## Integrating with Your Application

### OpenAI SDK (Python)

Point the OpenAI client at Majordomo instead of the OpenAI API:

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:7680/v1",
    api_key="your-openai-api-key",
    default_headers={
        "X-Majordomo-Key": "mdm_sk_your_key_here",
    }
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello!"}]
)
```

### OpenAI SDK (Node.js)

```javascript
import OpenAI from 'openai';

const client = new OpenAI({
  baseURL: 'http://localhost:7680/v1',
  apiKey: process.env.OPENAI_API_KEY,
  defaultHeaders: {
    'X-Majordomo-Key': 'mdm_sk_your_key_here',
  },
});

const response = await client.chat.completions.create({
  model: 'gpt-4o',
  messages: [{ role: 'user', content: 'Hello!' }],
});
```

### Anthropic SDK (Python)

```python
import anthropic

client = anthropic.Anthropic(
    base_url="http://localhost:7680",
    api_key="your-anthropic-api-key",
)

# Add Majordomo key via extra headers
response = client.messages.create(
    model="claude-sonnet-4-20250514",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Hello!"}],
    extra_headers={
        "X-Majordomo-Key": "mdm_sk_your_key_here",
    }
)
```

### Anthropic SDK (Node.js)

```javascript
import Anthropic from '@anthropic-ai/sdk';

const client = new Anthropic({
  baseURL: 'http://localhost:7680',
  apiKey: process.env.ANTHROPIC_API_KEY,
});

const response = await client.messages.create({
  model: 'claude-sonnet-4-20250514',
  max_tokens: 1024,
  messages: [{ role: 'user', content: 'Hello!' }],
}, {
  headers: {
    'X-Majordomo-Key': 'mdm_sk_your_key_here',
  },
});
```

---

## Adding Custom Metadata

Track usage by user, feature, environment, or any custom dimension using `X-Majordomo-*` headers:

```python
# Python example with OpenAI
client = OpenAI(
    base_url="http://localhost:7680/v1",
    api_key="your-openai-api-key",
)

response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello!"}],
    extra_headers={
        "X-Majordomo-Key": "mdm_sk_your_key_here",
        "X-Majordomo-User-Id": "user_123",
        "X-Majordomo-Feature": "chat",
        "X-Majordomo-Environment": "production",
    }
)
```

Query usage by metadata:

```sql
SELECT
    raw_metadata->>'User-Id' as user_id,
    COUNT(*) as requests,
    SUM(total_cost) as total_cost
FROM llm_requests
WHERE raw_metadata->>'User-Id' IS NOT NULL
GROUP BY raw_metadata->>'User-Id'
ORDER BY total_cost DESC;
```

---

## Managing API Keys

### List all keys

```bash
./bin/majordomo keys list
```

### View key details

```bash
./bin/majordomo keys get <key-id>
```

### Revoke a key

```bash
./bin/majordomo keys revoke <key-id>
```

Revoked keys immediately return `401 Unauthorized` on subsequent requests.

---

## Common Queries

### Total spend by model

```sql
SELECT
    model,
    COUNT(*) as requests,
    SUM(input_tokens) as input_tokens,
    SUM(output_tokens) as output_tokens,
    SUM(total_cost) as total_cost
FROM llm_requests
GROUP BY model
ORDER BY total_cost DESC;
```

### Daily spend

```sql
SELECT
    DATE(created_at) as date,
    COUNT(*) as requests,
    SUM(total_cost) as total_cost
FROM llm_requests
GROUP BY DATE(created_at)
ORDER BY date DESC
LIMIT 30;
```

### Spend by API key

```sql
SELECT
    ak.name as key_name,
    COUNT(*) as requests,
    SUM(lr.total_cost) as total_cost
FROM llm_requests lr
JOIN api_keys ak ON lr.majordomo_api_key_id = ak.id
GROUP BY ak.id, ak.name
ORDER BY total_cost DESC;
```

---

## S3 Body Storage

By default, Majordomo only logs metadata (token counts, costs, timing) to PostgreSQL. Enable S3 storage to capture full request and response bodies for debugging, compliance, or fine-tuning.

### Configuration

Add S3 settings to your `majordomo.yaml`:

```yaml
logging:
  body_storage: "s3"  # Enable S3 storage (default: "none")

s3:
  enabled: true
  bucket: "majordomo-logs"
  region: "us-east-1"
  access_key_id: ""      # Optional: uses AWS default credential chain if not set
  secret_access_key: ""
```

Or use environment variables:

```bash
export MAJORDOMO_LOGGING_BODY_STORAGE=s3
export MAJORDOMO_S3_ENABLED=true
export MAJORDOMO_S3_BUCKET=majordomo-logs
export MAJORDOMO_S3_REGION=us-east-1
export MAJORDOMO_S3_ACCESS_KEY_ID=your_access_key
export MAJORDOMO_S3_SECRET_ACCESS_KEY=your_secret_key
```

### S3 Object Structure

Bodies are stored as gzip-compressed JSON files with this path structure:

```
{api_key_prefix}/{date}/{request_id}.json.gz
```

For example:
```
a1b2c3d4-e5f6-78/2025-02-04/550e8400-e29b-41d4-a716-446655440000.json.gz
```

Each file contains:

```json
{
  "request_id": "550e8400-e29b-41d4-a716-446655440000",
  "timestamp": "2025-02-04T15:30:00Z",
  "request": {
    "method": "POST",
    "path": "/v1/chat/completions",
    "headers": {
      "Content-Type": "application/json"
    },
    "body": {
      "model": "gpt-4o",
      "messages": [{"role": "user", "content": "Hello!"}]
    }
  },
  "response": {
    "status_code": 200,
    "headers": {
      "Content-Type": "application/json"
    },
    "body": {
      "id": "chatcmpl-...",
      "choices": [...]
    }
  }
}
```

### Using S3-Compatible Storage

For local development or self-hosted storage, you can use MinIO, LocalStack, or Cloudflare R2:

```yaml
s3:
  enabled: true
  bucket: "majordomo-logs"
  region: "us-east-1"
  endpoint: "http://localhost:9000"  # MinIO endpoint
  access_key_id: "minioadmin"
  secret_access_key: "minioadmin"
```

### Retrieving Bodies

Use the AWS CLI to download and inspect bodies:

```bash
# List recent uploads
aws s3 ls s3://majordomo-logs/a1b2c3d4-e5f6-78/2025-02-04/ --recursive

# Download and decompress a specific request
aws s3 cp s3://majordomo-logs/a1b2c3d4-e5f6-78/2025-02-04/550e8400-e29b-41d4-a716-446655440000.json.gz - | gunzip | jq .
```

To find the S3 key for a specific request, query the `llm_requests` table:

```sql
SELECT s3_key FROM llm_requests WHERE id = '550e8400-e29b-41d4-a716-446655440000';
```

---

## Next Steps

- **Production deployment**: See the [Deployment guide](deployment.md) for Docker Compose, standalone Docker, and Kubernetes health probes
- **Pricing updates**: The gateway fetches pricing hourly from llm-prices.com automatically
