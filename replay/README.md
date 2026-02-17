# Replay

Fetch logged requests from the Majordomo Gateway database, replay them against a cheaper or faster model, and compare outputs. Helps you identify where you can safely downgrade models to reduce cost and latency.

## How It Works

1. **Fetch** — Queries the `llm_requests` table for requests matching your filters (metadata, model, time range). Supports bodies stored in PostgreSQL or S3.
2. **Replay** — Sends each original prompt to your target model via [majordomo-llm](../../majordomo-llm).
3. **Compare** — Checks if responses match exactly. For non-matches, an optional LLM judge determines if they're functionally equivalent.
4. **Report** — Prints a Rich terminal report with cost/latency comparisons, match rates, and divergent examples.

## Setup

```bash
cd majordomo-gateway/replay
uv sync
```

## Usage

```bash
# Uses replay.yaml in the current directory
uv run python -m replay.main

# Use a custom config path
uv run python -m replay.main --config path/to/config.yaml
```

## Configuration

Copy and edit `replay.yaml`:

```yaml
database:
  url: postgresql://user:pass@localhost:5432/majordomo  # or set DATABASE_URL env var

body_storage: postgres  # "postgres" or "s3"
s3:                      # required only when body_storage: s3
  bucket: my-bucket
  region: us-east-1

source:
  filters:               # raw_metadata key-value pairs (AND logic)
    feature: document-classification
  model: claude-sonnet-4-20250514  # optional — filter by original model
  days: 30
  limit: 50

target:
  provider: anthropic
  model: claude-haiku-4-20250514

judge:
  enabled: true
  provider: openai
  model: gpt-4.1-mini
```

### Configuration Reference

| Section | Key | Description |
|---------|-----|-------------|
| `database.url` | string | PostgreSQL connection URL. Falls back to `DATABASE_URL` env var. |
| `body_storage` | `postgres` or `s3` | Where request/response bodies are stored. |
| `s3.bucket` | string | S3 bucket name (required when `body_storage: s3`). |
| `s3.region` | string | AWS region for the S3 bucket. |
| `source.filters` | map | Arbitrary `raw_metadata` key-value pairs. All must match (AND logic). |
| `source.model` | string | Filter to requests originally made with this model. Optional. |
| `source.days` | int | Look back this many days. Default: 30. |
| `source.limit` | int | Maximum number of requests to replay. Default: 50. |
| `target.provider` | string | LLM provider for the replay model (`anthropic`, `openai`, `gemini`, etc.). |
| `target.model` | string | Model to replay requests against. |
| `judge.enabled` | bool | Enable LLM-as-judge for non-exact matches. |
| `judge.provider` | string | Provider for the judge model. |
| `judge.model` | string | Model used as the judge. |

## Project Structure

```
src/replay/
├── main.py      # CLI entry point, orchestrates the workflow
├── fetch.py     # Query PostgreSQL, optional S3 body retrieval
├── parse.py     # Parse OpenAI/Anthropic/Gemini request/response formats
├── compare.py   # Exact match + LLM-as-judge comparison
└── report.py    # Rich terminal report
```

## Supported Providers

Request/response body parsing supports:

- **OpenAI** (and compatible APIs like DeepSeek)
- **Anthropic**
- **Gemini**

The target model for replay can be any provider supported by majordomo-llm.
