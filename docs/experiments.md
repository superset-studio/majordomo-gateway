# A/B Testing with Experiments

Majordomo Gateway supports live traffic splitting between LLM model variants. This lets you compare models on real production traffic — measuring latency, cost, and error rates — without changing your application code.

## How it works

When an experiment is active, the gateway intercepts requests matching your API key and rewrites the `model` field before forwarding upstream. Each request is assigned to a variant based on configured weights. Results are tracked automatically via request metadata.

```
Client request (model: "gpt-4o")
        │
        ▼
  ┌─────────────────────────────────┐
  │  Experiment: "Model comparison" │
  │  ├─ control    gpt-4o      50%  │
  │  └─ challenger gpt-4o-mini 50%  │
  └─────────────────────────────────┘
        │
   weighted random
        │
   ┌────┴────┐
   │         │
   ▼         ▼
 gpt-4o  gpt-4o-mini
   │         │
   └────┬────┘
        │
  upstream provider
```

Experiment metadata is stored in `indexed_metadata` on every request log, so you can query results directly or use the built-in results endpoint.

## Enabling experiments

Add to `majordomo.yaml`:

```yaml
experiments:
  enabled: true
  cache_ttl: 5m  # how long active experiments are cached per API key (default: 5m)
```

Or via environment variable:

```bash
export MAJORDOMO_EXPERIMENTS_ENABLED=true
```

The JWT secret must also be configured to access the Admin API.

## Managing experiments via API

All experiment management is done through the Admin API under `/api/v1/admin/experiments`. Requests require a JWT token from `POST /api/v1/admin/login`.

### Create an experiment

```bash
curl -X POST http://localhost:7680/api/v1/admin/experiments \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "GPT-4o vs GPT-4o-mini",
    "description": "Cost/quality tradeoff test",
    "apiKeyId": "your-api-key-uuid"
  }'
```

Scope options:
- `apiKeyId` — experiment applies to one specific API key
- omit `apiKeyId` — experiment applies to all keys for your user/org (only one active experiment allowed at a time per scope)

### Add variants

```bash
# Control variant (your current model)
curl -X POST http://localhost:7680/api/v1/admin/experiments/{id}/variants \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "control",
    "provider": "openai",
    "model": "gpt-4o",
    "weight": 50,
    "isControl": true
  }'

# Challenger variant
curl -X POST http://localhost:7680/api/v1/admin/experiments/{id}/variants \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "challenger",
    "provider": "openai",
    "model": "gpt-4o-mini",
    "weight": 50
  }'
```

`weight` is relative — `50/50` gives an equal split; `70/30` routes 70% to the first variant.

### Activate

```bash
curl -X POST http://localhost:7680/api/v1/admin/experiments/{id}/activate \
  -H "Authorization: Bearer $JWT_TOKEN"
```

Activation validates that:
- At least 2 variants exist
- Total weight is > 0
- No other experiment is already active for the same scope

### Pause / Complete

```bash
# Pause (can be re-activated)
curl -X POST http://localhost:7680/api/v1/admin/experiments/{id}/pause \
  -H "Authorization: Bearer $JWT_TOKEN"

# Complete (terminal state)
curl -X POST http://localhost:7680/api/v1/admin/experiments/{id}/complete \
  -H "Authorization: Bearer $JWT_TOKEN"
```

## Cross-provider experiments

You can route between providers. Currently supported: **OpenAI-format → Anthropic**.

```bash
# Variant targeting Anthropic from an OpenAI-format client
curl -X POST http://localhost:7680/api/v1/admin/experiments/{id}/variants \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "anthropic-challenger",
    "provider": "anthropic",
    "model": "claude-haiku-4-5",
    "weight": 20
  }'
```

The gateway uses its existing OpenAI→Anthropic translation layer, so your application sends standard OpenAI-format requests unchanged.

## Sticky assignment

By default, traffic is split randomly on each request. Enable `sticky` to ensure the same subject always gets the same variant — useful for user-facing features where consistency matters.

```bash
curl -X POST http://localhost:7680/api/v1/admin/experiments \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Sticky user test",
    "sticky": true,
    "stickyKeyHeader": "user-id"
  }'
```

With `stickyKeyHeader: "user-id"`, the gateway hashes `X-Majordomo-User-Id` from each request to determine which variant to assign. Without `stickyKeyHeader`, the API key itself is used as the sticky identity.

## Viewing results

```bash
curl -X POST http://localhost:7680/api/v1/admin/experiments/{id}/results \
  -H "Authorization: Bearer $JWT_TOKEN"
```

Response:

```json
{
  "experimentId": "...",
  "experimentName": "GPT-4o vs GPT-4o-mini",
  "totalRequests": 1000,
  "variants": [
    {
      "variantName": "control",
      "requestCount": 503,
      "avgLatencyMs": 1240.5,
      "totalCost": 0.0821,
      "avgInputTokens": 412.3,
      "avgOutputTokens": 87.1,
      "errorCount": 2,
      "errorRate": 0.004
    },
    {
      "variantName": "challenger",
      "requestCount": 497,
      "avgLatencyMs": 680.2,
      "totalCost": 0.0098,
      "avgInputTokens": 412.3,
      "avgOutputTokens": 84.9,
      "errorCount": 1,
      "errorRate": 0.002
    }
  ]
}
```

### Querying results directly

Experiment metadata is stored in `indexed_metadata` under three keys:

| Key | Value |
|-----|-------|
| `_experiment-id` | UUID of the experiment |
| `_experiment-variant` | Name of the assigned variant |
| `_experiment-original-model` | Model the client originally requested |

To activate these keys for GIN-indexed queries, use the metadata key activation API:

```bash
curl -X PUT http://localhost:7680/api/v1/admin/api-keys/{keyId}/metadata-keys/_experiment-variant \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"isActive": true}'
```

Then query directly:

```sql
SELECT
    indexed_metadata->>'_experiment-variant' AS variant,
    COUNT(*)                                  AS requests,
    AVG(response_time_ms)                     AS avg_latency_ms,
    SUM(total_cost)                           AS total_cost,
    AVG(output_tokens)                        AS avg_output_tokens
FROM llm_requests
WHERE indexed_metadata->>'_experiment-id' = 'your-experiment-uuid'
GROUP BY indexed_metadata->>'_experiment-variant';
```

## Full API reference

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/admin/experiments` | Create experiment |
| `GET` | `/api/v1/admin/experiments` | List experiments |
| `GET` | `/api/v1/admin/experiments/{id}` | Get experiment with variants |
| `PUT` | `/api/v1/admin/experiments/{id}` | Update name/description/sticky |
| `DELETE` | `/api/v1/admin/experiments/{id}` | Delete (draft/completed only) |
| `POST` | `/api/v1/admin/experiments/{id}/variants` | Add variant |
| `PUT` | `/api/v1/admin/experiments/{id}/variants/{variantId}` | Update variant |
| `DELETE` | `/api/v1/admin/experiments/{id}/variants/{variantId}` | Delete variant |
| `POST` | `/api/v1/admin/experiments/{id}/activate` | Activate experiment |
| `POST` | `/api/v1/admin/experiments/{id}/pause` | Pause experiment |
| `POST` | `/api/v1/admin/experiments/{id}/complete` | Complete experiment |
| `POST` | `/api/v1/admin/experiments/{id}/results` | Get per-variant results |
