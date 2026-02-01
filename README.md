# Majordomo Gateway

A lightweight LLM API gateway that proxies requests to upstream providers (OpenAI, Anthropic, Google Gemini), logs usage metrics, and calculates costs.

## Features

- **Multi-provider support** - Route requests to OpenAI, Anthropic, and Google Gemini
- **Automatic cost calculation** - Track spending with real-time pricing data from [llm-prices.com](https://llm-prices.com)
- **Usage logging** - Store request logs in PostgreSQL with token counts, latency, and costs
- **Custom metadata** - Attach custom headers (`X-Majordomo-*`) for tracking by user, feature, environment, etc.
- **Body storage** - Optionally store full request/response bodies in S3 or PostgreSQL
- **Zero-config provider detection** - Automatically detects provider from request path

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

### 4. Run

```bash
make run
```

The gateway starts on `http://localhost:7680` by default.

### 5. Make a request

```bash
curl -X POST http://localhost:7680/v1/chat/completions \
  -H "X-Majordomo-Key: my-app-key" \
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
| `X-Majordomo-Key` | Yes | Your API key (used for tracking, any non-empty value works) |
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
  -H "X-Majordomo-Key: my-key" \
  -H "X-Majordomo-User-Id: user_123" \
  -H "X-Majordomo-Feature: chat" \
  -H "X-Majordomo-Environment: production" \
  ...
```

Metadata is stored in the `raw_metadata` JSONB column.

## Docker

### Build

```bash
docker build -t majordomo-gateway .
```

### Run

```bash
docker run -p 7680:7680 \
  -e MAJORDOMO_STORAGE_POSTGRES_HOST=host.docker.internal \
  -e MAJORDOMO_STORAGE_POSTGRES_USER=postgres \
  -e MAJORDOMO_STORAGE_POSTGRES_PASSWORD=secret \
  majordomo-gateway
```

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
2. Gateway detects provider from path or `X-Majordomo-Provider` header
3. Request is forwarded to upstream provider
4. Response is parsed for token usage
5. Cost is calculated using current pricing data
6. Request log is written to PostgreSQL asynchronously
7. (Optional) Full request/response bodies stored in S3
8. Response is returned to client

## Database schema

The gateway uses two tables:

- `llm_requests` - Request logs with token counts, costs, and metadata
- `llm_requests_metadata_keys` - Tracks metadata keys for selective indexing

See [schema.sql](schema.sql) for the full schema.

## Development

```bash
make build       # Build binary
make run         # Build and run
make test        # Run tests
make test-cover  # Run tests with coverage
make lint        # Run linter
make fmt         # Format code
```

## License

MIT License - see [LICENSE](LICENSE) for details.
