# Proxy Keys

Proxy keys let you issue customer-facing API keys (`mdm_pk_...`) that the gateway swaps for real provider credentials before forwarding upstream. Your customers never see or handle provider API keys directly.

```
Customer's Agent  →  Authorization: Bearer mdm_pk_xxx
                     X-Majordomo-Key: mdm_sk_yyy
                          ↓
                 Gateway validates mdm_sk_yyy
                 Gateway looks up mdm_pk_xxx → finds real key for detected provider
                 Gateway replaces Authorization header
                          ↓
                 Upstream LLM Provider (OpenAI, Anthropic, Gemini)
```

## When to Use Proxy Keys

- **Multi-tenant platforms** — give each customer their own key without sharing provider credentials
- **Key rotation** — rotate provider keys centrally without updating every client
- **Per-customer cost tracking** — each proxy key tracks its own request count and last-used timestamp
- **Access control** — revoke a single customer's access without affecting others

## Prerequisites

- A running Majordomo Gateway with PostgreSQL ([Getting Started](getting-started.md))
- The `proxy_keys` and `proxy_key_provider_mappings` tables applied from `schema.sql`
- A 32-byte AES-256 encryption key for securing stored provider credentials

## Setup

### 1. Generate an Encryption Key

Provider API keys are encrypted at rest using AES-256-GCM. Generate a key:

```bash
openssl rand -hex 32
```

### 2. Configure the Encryption Key

Add the key to your `majordomo.yaml`:

```yaml
secrets:
  encryption_key: "your-64-char-hex-key-here"
```

Or set the environment variable:

```bash
export MAJORDOMO_SECRETS_ENCRYPTION_KEY="your-64-char-hex-key-here"
```

!!! tip "Keep this key safe"
    If you lose the encryption key, all stored provider API keys become unrecoverable. Back it up securely.

### 3. Apply the Schema

If you haven't already, apply the latest schema to add the proxy key tables:

```bash
psql -U postgres -d majordomo -f schema.sql
```

This adds three things:

| Object | Purpose |
|--------|---------|
| `proxy_keys` table | Stores proxy key hashes, names, and ownership |
| `proxy_key_provider_mappings` table | Maps each proxy key to encrypted provider keys per provider |
| `proxy_key_id` column on `llm_requests` | Links request logs to the proxy key that was used |

### 4. Restart the Gateway

Restart the gateway. You should see:

```
proxy key support enabled
```

If the encryption key is not configured, proxy key support is silently disabled and the gateway operates normally.

---

## Managing Proxy Keys (CLI)

### Create a Proxy Key

Each proxy key belongs to a Majordomo API key (the "operator" key):

```bash
./bin/majordomo proxy-keys create \
  --name "Customer 1" \
  --majordomo-key-id a1b2c3d4-e5f6-7890-abcd-ef1234567890 \
  --description "Production access for Customer 1"
```

Output:

```
Proxy key created successfully!

ID:                b2c3d4e5-f6a7-8901-bcde-f12345678901
Name:              Customer 1
Majordomo Key ID:  a1b2c3d4-e5f6-7890-abcd-ef1234567890

IMPORTANT: Save this key - it will not be shown again:

  mdm_pk_abc123def456...
```

### Set a Provider Mapping

Map the proxy key to a real provider API key:

```bash
./bin/majordomo proxy-keys set-provider b2c3d4e5-f6a7-8901-bcde-f12345678901 \
  --provider openai \
  --api-key sk-proj-real-openai-key
```

You can map multiple providers to the same proxy key:

```bash
./bin/majordomo proxy-keys set-provider b2c3d4e5-f6a7-8901-bcde-f12345678901 \
  --provider anthropic \
  --api-key sk-ant-real-anthropic-key
```

### List Proxy Keys

```bash
./bin/majordomo proxy-keys list --majordomo-key-id a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

### View Proxy Key Details

```bash
./bin/majordomo proxy-keys get b2c3d4e5-f6a7-8901-bcde-f12345678901
```

### List Provider Mappings

```bash
./bin/majordomo proxy-keys list-providers b2c3d4e5-f6a7-8901-bcde-f12345678901
```

### Remove a Provider Mapping

```bash
./bin/majordomo proxy-keys remove-provider b2c3d4e5-f6a7-8901-bcde-f12345678901 \
  --provider openai
```

### Revoke a Proxy Key

```bash
./bin/majordomo proxy-keys revoke b2c3d4e5-f6a7-8901-bcde-f12345678901
```

Revoked keys immediately return `401 Unauthorized`.

---

## Managing Proxy Keys (REST API)

All endpoints require the `X-Majordomo-Key` header. A Majordomo key can only manage its own proxy keys.

### Create a Proxy Key

```bash
curl -X POST http://localhost:7680/api/v1/proxy-keys \
  -H "X-Majordomo-Key: mdm_sk_your_key" \
  -H "Content-Type: application/json" \
  -d '{"name": "Customer 1", "description": "Production access"}'
```

Response includes the plaintext key (shown once):

```json
{
  "id": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
  "name": "Customer 1",
  "key": "mdm_pk_abc123def456...",
  "is_active": true,
  "created_at": "2025-02-04T12:00:00Z"
}
```

### List Proxy Keys

```bash
curl http://localhost:7680/api/v1/proxy-keys \
  -H "X-Majordomo-Key: mdm_sk_your_key"
```

### Get a Proxy Key

```bash
curl http://localhost:7680/api/v1/proxy-keys/{id} \
  -H "X-Majordomo-Key: mdm_sk_your_key"
```

### Revoke a Proxy Key

```bash
curl -X DELETE http://localhost:7680/api/v1/proxy-keys/{id} \
  -H "X-Majordomo-Key: mdm_sk_your_key"
```

### Set a Provider Mapping

```bash
curl -X PUT http://localhost:7680/api/v1/proxy-keys/{id}/providers/openai \
  -H "X-Majordomo-Key: mdm_sk_your_key" \
  -H "Content-Type: application/json" \
  -d '{"api_key": "sk-proj-real-openai-key"}'
```

The provider API key is encrypted before storage and never returned in responses.

### List Provider Mappings

```bash
curl http://localhost:7680/api/v1/proxy-keys/{id}/providers \
  -H "X-Majordomo-Key: mdm_sk_your_key"
```

Response shows providers without keys:

```json
[
  {"id": "...", "provider": "openai", "created_at": "...", "updated_at": "..."},
  {"id": "...", "provider": "anthropic", "created_at": "...", "updated_at": "..."}
]
```

### Remove a Provider Mapping

```bash
curl -X DELETE http://localhost:7680/api/v1/proxy-keys/{id}/providers/openai \
  -H "X-Majordomo-Key: mdm_sk_your_key"
```

---

## Using Proxy Keys in Your Application

Once a proxy key has a provider mapping, use it in place of a real provider API key. The gateway detects the `mdm_pk_` prefix, looks up the real key, and swaps it before forwarding.

### OpenAI SDK (Python)

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:7680/v1",
    api_key="mdm_pk_customer_proxy_key",  # Proxy key instead of real key
    default_headers={
        "X-Majordomo-Key": "mdm_sk_operator_key",
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
  apiKey: 'mdm_pk_customer_proxy_key',
  defaultHeaders: {
    'X-Majordomo-Key': 'mdm_sk_operator_key',
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
    api_key="mdm_pk_customer_proxy_key",
)

response = client.messages.create(
    model="claude-sonnet-4-20250514",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Hello!"}],
    extra_headers={
        "X-Majordomo-Key": "mdm_sk_operator_key",
    }
)
```

### curl

```bash
curl -X POST http://localhost:7680/v1/chat/completions \
  -H "X-Majordomo-Key: mdm_sk_operator_key" \
  -H "Authorization: Bearer mdm_pk_customer_proxy_key" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "hello"}]}'
```

---

## Querying Proxy Key Usage

### Requests by proxy key

```sql
SELECT
    pk.name as proxy_key_name,
    COUNT(*) as requests,
    SUM(lr.total_cost) as total_cost
FROM llm_requests lr
JOIN proxy_keys pk ON lr.proxy_key_id = pk.id
GROUP BY pk.id, pk.name
ORDER BY total_cost DESC;
```

### Recent requests for a specific proxy key

```sql
SELECT model, input_tokens, output_tokens, total_cost, requested_at
FROM llm_requests
WHERE proxy_key_id = 'b2c3d4e5-f6a7-8901-bcde-f12345678901'
ORDER BY requested_at DESC
LIMIT 10;
```

---

## Edge Cases

| Scenario | Behavior |
|----------|----------|
| Authorization header has no `mdm_pk_` prefix | Passthrough — existing behavior unchanged |
| Proxy key not found | `401 Unauthorized` |
| Proxy key revoked | `401 Unauthorized` |
| Proxy key belongs to a different Majordomo key | `401 Unauthorized` |
| No provider mapping for the detected provider | `401` with "no provider key configured for {provider}" |
| Encryption key not configured | Proxy key support disabled; all requests pass through normally |
| Proxy key used without `X-Majordomo-Key` | `401` (existing Majordomo key check fails first) |

---

## Security Notes

- Provider API keys are encrypted with AES-256-GCM (authenticated encryption) before storage
- Proxy key hashes are stored using SHA-256 — the plaintext is only shown at creation time
- Resolved provider keys are cached in memory for 5 minutes to reduce database lookups
- The REST API never returns provider API keys in responses
