# Majordomo Gateway

[![CI](https://github.com/superset-studio/majordomo-gateway/actions/workflows/ci.yaml/badge.svg)](https://github.com/superset-studio/majordomo-gateway/actions/workflows/ci.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/superset-studio/majordomo-gateway)](https://goreportcard.com/report/github.com/superset-studio/majordomo-gateway)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A lightweight LLM API gateway that proxies requests to upstream providers (OpenAI, Anthropic, Google Gemini), logs usage metrics, and calculates costs.

## Features

- **Multi-provider support** - Route requests to OpenAI, Anthropic, and Google Gemini
- **API key management** - Create, list, and revoke API keys with built-in CLI commands
- **Automatic cost calculation** - Track spending with real-time pricing data from [llm-prices.com](https://llm-prices.com)
- **Usage logging** - Store request logs in PostgreSQL with token counts, latency, and costs
- **Custom metadata** - Attach custom headers (`X-Majordomo-*`) for tracking by user, feature, environment, etc.
- **Body storage** - Optionally store full request/response bodies in S3 or PostgreSQL
- **Zero-config provider detection** - Automatically detects provider from request path

## Documentation

- **[Getting Started Guide](docs/getting-started.md)** - Full walkthrough with SDK integration examples

## Quick Start

### Prerequisites

- Go 1.25+
- PostgreSQL 14+
- (Optional) S3-compatible storage for body logging

### 1. Clone and build

```bash
git clone https://github.com/superset-studio/majordomo-gateway.git
cd majordomo-gateway
make build
```

### 2. Set up the database

```bash
psql -U postgres -c "CREATE DATABASE majordomo;"
psql -U postgres -d majordomo -f schema.sql
```

### 3. Configure

Copy the example environment file and edit it:

```bash
cp .env.example .env
# Edit .env with your PostgreSQL credentials
```

### 4. Create an API key

```bash
./bin/majordomo keys create --name "My First Key"
```

Save the returned key (it won't be shown again). Keys have the format `mdm_sk_...`.

### 5. Run

```bash
make run
```

The gateway starts on `http://localhost:7680` by default.

### 6. Make a request

```bash
curl -X POST http://localhost:7680/v1/chat/completions \
  -H "X-Majordomo-Key: mdm_sk_your_key_here" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Configuration

Configuration is loaded from `majordomo.yaml` (or `/etc/majordomo/majordomo.yaml`). Environment variables with `MAJORDOMO_` prefix override config file values.

### Example configuration

```yaml
server:
  host: "0.0.0.0"
  port: 7680
  read_timeout: 30s
  write_timeout: 120s

storage:
  postgres:
    host: localhost
    port: 5432
    database: majordomo
    max_conns: 20

logging:
  body_storage: "none"  # "none", "postgres", or "s3"

s3:
  enabled: true
  bucket: "majordomo-logs"
  region: "us-east-1"

pricing:
  remote_url: "https://www.llm-prices.com/current-v1.json"
  refresh_interval: 1h
```

### Environment variables

| Variable | Description |
|----------|-------------|
| `MAJORDOMO_STORAGE_POSTGRES_HOST` | PostgreSQL host |
| `MAJORDOMO_STORAGE_POSTGRES_PORT` | PostgreSQL port |
| `MAJORDOMO_STORAGE_POSTGRES_USER` | PostgreSQL user |
| `MAJORDOMO_STORAGE_POSTGRES_PASSWORD` | PostgreSQL password |
| `MAJORDOMO_STORAGE_POSTGRES_DATABASE` | PostgreSQL database |
| `MAJORDOMO_S3_ACCESS_KEY_ID` | AWS/S3 access key |
| `MAJORDOMO_S3_SECRET_ACCESS_KEY` | AWS/S3 secret key |

## Usage

### Headers

| Header | Required | Description |
|--------|----------|-------------|
| `X-Majordomo-Key` | Yes | Your Majordomo API key (`mdm_sk_...`), validated against the database |
| `X-Majordomo-Provider` | No | Force a specific provider (`openai`, `anthropic`, `gemini`) |
| `X-Majordomo-*` | No | Custom metadata (stored with request log) |
| `Authorization` | Yes | Upstream provider API key (`Bearer sk-...`) |

### Provider detection

The gateway automatically detects the provider from the request path:

| Path pattern | Provider |
|--------------|----------|
| `/v1/chat/completions` | OpenAI |
| `/v1/messages` | Anthropic |
| `*generateContent*` | Gemini |

Override with `X-Majordomo-Provider` header if needed.

### Custom metadata

Attach metadata to requests for analytics:

```bash
curl -X POST http://localhost:7680/v1/chat/completions \
  -H "X-Majordomo-Key: mdm_sk_your_key_here" \
  -H "X-Majordomo-User-Id: user_123" \
  -H "X-Majordomo-Feature: chat" \
  -H "X-Majordomo-Environment: production" \
  ...
```

Metadata is stored in the `raw_metadata` JSONB column.

## CLI Commands

### API Key Management

Create and manage API keys using the `majordomo keys` command:

```bash
# Create a new API key
majordomo keys create --name "Production API"
majordomo keys create --name "Dev Key" --description "For local development"

# List all API keys
majordomo keys list

# Get details for a specific key
majordomo keys get <key-id>

# Update key metadata
majordomo keys update <key-id> --name "New Name"
majordomo keys update <key-id> --description "Updated description"

# Revoke a key (permanent)
majordomo keys revoke <key-id>
```

API keys use the format `mdm_sk_<random>`. The plaintext key is only shown once at creation time - store it securely. Keys are validated on every request and cached in memory for 5 minutes.

## Deployment

### Docker Compose (recommended)

The quickest way to run the gateway with PostgreSQL:

```bash
cp .env.example .env
# Edit .env — set MAJORDOMO_STORAGE_POSTGRES_PASSWORD at minimum

make compose-up    # or: docker compose up --build -d
```

The gateway is available at `http://localhost:7680`. The database schema is applied automatically on first start.

```bash
# Verify it's running
curl http://localhost:7680/readyz    # {"status":"ok"}

# Stop everything
make compose-down
```

### Docker (standalone)

If you already have a PostgreSQL instance:

```bash
docker build -t majordomo-gateway .

docker run -p 7680:7680 \
  -e MAJORDOMO_STORAGE_POSTGRES_HOST=host.docker.internal \
  -e MAJORDOMO_STORAGE_POSTGRES_USER=postgres \
  -e MAJORDOMO_STORAGE_POSTGRES_PASSWORD=secret \
  majordomo-gateway
```

### Health endpoints

| Endpoint | Purpose | Healthy | Unhealthy |
|----------|---------|---------|-----------|
| `GET /health` | Liveness probe | `200 ok` | — |
| `GET /readyz` | Readiness probe (pings DB) | `200 {"status":"ok"}` | `503 {"status":"error",...}` |

## Architecture

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Client    │────▶│ Majordomo Gateway│────▶│  LLM Provider   │
└─────────────┘     └──────────────────┘     └─────────────────┘
                            │
                ┌───────────┴───────────┐
                ▼                       ▼
        ┌──────────────┐        ┌─────────────┐
        │  PostgreSQL  │        │     S3      │
        │ (logs, costs,│        │  (request/  │
        │   metadata)  │        │   response  │
        └──────────────┘        │   bodies)   │
                                └─────────────┘
```

### Request flow

1. Client sends request with `X-Majordomo-Key` header
2. Gateway validates the API key against the database (returns 401 if invalid/revoked)
3. Gateway detects provider from path or `X-Majordomo-Provider` header
4. Request is forwarded to upstream provider
5. Response is parsed for token usage
6. Cost is calculated using current pricing data
7. Request log is written to PostgreSQL asynchronously (linked to API key)
8. (Optional) Full request/response bodies stored in S3
9. Response is returned to client

## Database schema

The gateway uses three tables:

- `api_keys` - Majordomo API keys with hashes, status, and usage counts
- `llm_requests` - Request logs with token counts, costs, and metadata (references `api_keys`)
- `llm_requests_metadata_keys` - Tracks metadata keys for selective indexing

See [schema.sql](schema.sql) for the full schema.

## Development

```bash
make build         # Build binary
make run           # Build and run
make test          # Run tests
make test-cover    # Run tests with coverage
make lint          # Run linter
make fmt           # Format code
make compose-up    # Start gateway + postgres via Docker Compose
make compose-down  # Stop compose stack
```

## License

MIT License - see [LICENSE](LICENSE) for details.
