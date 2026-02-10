# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
make build         # Build binary to bin/majordomo-proxy
make run           # Build and run the server
make test          # Run all tests
make test-cover    # Run tests with coverage report
make lint          # Run golangci-lint
make fmt           # Format code
make vet           # Run go vet
make compose-up    # Start gateway + postgres via Docker Compose
make compose-down  # Stop compose stack
```

Run a single test: `go test -v ./internal/pricing -run TestCalculate`

## Architecture

Majordomo Gateway is an LLM API proxy that routes requests to upstream providers, logs usage metrics, and calculates costs.

### Request Flow

1. **Authentication** (`internal/auth/resolver.go`): API key from `X-Majordomo-Key` header is SHA256-hashed. The hash is used to track requests and associate metadata.

2. **Provider Detection** (`internal/provider/provider.go`): Provider is determined by explicit `X-Majordomo-Provider` header or inferred from request path (e.g., `/v1/messages` → Anthropic, `/v1/chat/completions` → OpenAI).

3. **Proxying** (`internal/proxy/handler.go`): Request is forwarded to the upstream provider. Response is optionally gzip-compressed before returning to client.

4. **Logging** (async): Response is parsed by provider-specific parsers (`internal/provider/`) to extract token counts and model name. Cost is calculated using pricing data, and the request is logged to PostgreSQL via a buffered channel.

### Key Components

- **Pricing Service** (`internal/pricing/service.go`): Fetches pricing from remote URL (llm-prices.com), refreshes hourly. Uses `model_aliases.json` to map provider-returned model names (e.g., `claude-sonnet-4-5-20250929`) to canonical pricing names (e.g., `claude-sonnet-4.5`). Falls back to `pricing.json` if remote fetch fails.

- **PostgreSQL Storage** (`internal/storage/postgres.go`): Async write loop with 1000-entry buffer. Maintains HyperLogLog sketches for metadata cardinality estimation. Active metadata keys are cached for selective indexing.

- **S3 Body Storage** (`internal/storage/s3.go`): Optional storage for full request/response bodies. Enable via `logging.body_storage: "s3"` in config.

### Configuration

Config is loaded from `majordomo.yaml` (or `/etc/majordomo/majordomo.yaml`). Environment variables override config with `MAJORDOMO_` prefix (e.g., `MAJORDOMO_STORAGE_POSTGRES_PASSWORD`).

### Database Schema

- **`llm_requests`**: Request log table with token counts, costs, timing, and metadata. Indexed by API key hash and request time.
- **`llm_requests_metadata_keys`**: Per-API-key metadata configuration. Tracks which metadata keys are active (GIN-indexed) vs. raw (stored but not indexed). Includes HyperLogLog state for cardinality estimation.

### Custom Metadata

Headers prefixed with `X-Majordomo-` (except `-Key` and `-Provider`) are stored as metadata on request logs. Active keys (configured per API key) are copied to `indexed_metadata` column for GIN-indexed queries.

### Health Endpoints

- `GET /health` — Liveness probe. Always returns `200 ok`.
- `GET /readyz` — Readiness probe. Pings PostgreSQL; returns `200 {"status":"ok"}` or `503 {"status":"error","error":"..."}`.

## Deployment

### Docker Compose

The quickest way to run the gateway with a database:

1. Copy `.env.example` to `.env` and set `MAJORDOMO_STORAGE_POSTGRES_PASSWORD`.
2. Run `make compose-up` (or `docker compose up --build -d`).
3. The gateway is available at `http://localhost:7680`.

`schema.sql` is automatically applied to Postgres on first start. The gateway waits for Postgres to be healthy before starting.
