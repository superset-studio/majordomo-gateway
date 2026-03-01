# Claude Code + Majordomo Gateway

This guide shows how to route [Claude Code](https://docs.anthropic.com/en/docs/claude-code) through Majordomo Gateway to track usage and costs across your development team. There are two approaches depending on the level of tracking you need.

## Two Approaches

| | Simple Setup | Companion Setup |
|---|---|---|
| **How** | Environment variables + `settings.json` | `majordomo-companion` local proxy |
| **Tracks** | Per-request tokens, costs, model, metadata | Everything in Simple, plus session grouping |
| **Session tracking** | No | Yes — groups requests into sessions with aggregates |
| **Request detail parsing** | Yes — tool usage, thinking, plan mode | Yes |
| **Setup effort** | Minimal — edit one file | Run a command before each session |
| **Best for** | Individual developers, simple cost tracking | Teams wanting session-level analytics |

Both approaches log every request to `llm_requests` and parse Claude Code-specific metadata (tool usage, thinking mode, plan mode) into `claude_request_details`. The companion adds session grouping on top.

```
Simple:      Claude Code  →  Majordomo Gateway  →  Anthropic API
                                    ↓
                              PostgreSQL

Companion:   Claude Code  →  companion (local proxy)  →  Majordomo Gateway  →  Anthropic API
                                                              ↓
                                                        PostgreSQL
```

## Prerequisites

- A running Majordomo Gateway instance ([Getting Started](getting-started.md) or [Docker Compose](deployment/docker-compose.md))
- A Majordomo API key (`mdm_sk_...`)
- Claude Code installed (`npm install -g @anthropic-ai/claude-code`)
- An Anthropic API key

---

## Simple Setup (No Companion)

Point Claude Code at the gateway using environment variables. No extra tooling required.

### Option A: Persistent via `settings.json`

Edit `~/.claude/settings.json` to set the gateway URL and tracking headers for every session:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:7680",
    "ANTHROPIC_CUSTOM_HEADERS": "X-Majordomo-Key: mdm_sk_your_key_here"
  }
}
```

To add metadata (developer name, project, team), add more headers separated by newlines:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:7680",
    "ANTHROPIC_CUSTOM_HEADERS": "X-Majordomo-Key: mdm_sk_your_key_here\nX-Majordomo-Developer: alice\nX-Majordomo-Project: backend-api\nX-Majordomo-Team: platform"
  }
}
```

Then just run Claude Code as usual:

```bash
claude
```

### Option B: Inline Environment Variables

For one-off sessions or testing, set variables inline:

```bash
ANTHROPIC_BASE_URL=http://localhost:7680 \
ANTHROPIC_CUSTOM_HEADERS="X-Majordomo-Key: mdm_sk_your_key_here" \
claude
```

!!! tip "Don't mix both"
    `ANTHROPIC_CUSTOM_HEADERS` is a single string value, not an array. If set in both `settings.json` and the shell, one will override the other entirely — the headers won't merge. Pick one location and stick with it.

### Verify It Works

After running Claude Code, check that requests are being logged:

```sql
SELECT model, input_tokens, output_tokens, total_cost, requested_at
FROM llm_requests
ORDER BY requested_at DESC
LIMIT 5;
```

---

## Companion Setup (With Session Tracking)

The companion proxy (`majordomo-companion`) wraps each Claude Code session with lifecycle tracking. It registers a session on startup, injects the session ID into every request, and ends the session on shutdown.

### Build the Companion

```bash
make build-companion
```

This creates `./bin/majordomo-companion`.

### Quick Start with `exec`

The simplest way — start the proxy, run Claude Code, clean up automatically:

```bash
majordomo-companion exec \
  --gateway http://localhost:7680 \
  --api-key mdm_sk_your_key_here \
  -- claude
```

The companion:

1. Registers a new session with the gateway
2. Starts a local proxy on a random port
3. Launches `claude` with `ANTHROPIC_BASE_URL` pointing at the local proxy
4. Injects `X-Majordomo-Key` and `X-Majordomo-Session-Id` headers into every request
5. Waits for Claude Code to exit
6. Ends the session and shuts down

You can pass any arguments to Claude Code after the `--`:

```bash
majordomo-companion exec \
  --gateway http://localhost:7680 \
  --api-key mdm_sk_your_key_here \
  -- claude "Explain this codebase"
```

### Adding Metadata

#### Via CLI Flags

Use repeatable `--metadata` flags:

```bash
majordomo-companion exec \
  --gateway http://localhost:7680 \
  --api-key mdm_sk_your_key_here \
  --metadata Developer=alice \
  --metadata Project=backend-api \
  --metadata Team=platform \
  -- claude
```

#### Via Metadata File

Create a `.majordomo/metadata.json` file in your project directory:

```json
{
  "Project": "backend-api",
  "Team": "platform"
}
```

The companion reads this file automatically from the current working directory. CLI `--metadata` flags override file values when both specify the same key.

You can specify a different directory with `--workdir`:

```bash
majordomo-companion exec \
  --gateway http://localhost:7680 \
  --api-key mdm_sk_your_key_here \
  --workdir /path/to/project \
  --metadata Developer=alice \
  -- claude
```

### Long-Running Setup with `start` / `stop`

For workflows where you want the proxy running across multiple Claude Code invocations:

**Start the companion:**

```bash
majordomo-companion start \
  --gateway http://localhost:7680 \
  --api-key mdm_sk_your_key_here \
  --pid-file /tmp/companion.pid \
  --env-file /tmp/companion.env \
  --metadata Developer=alice
```

This prints the local proxy URL (e.g., `http://127.0.0.1:54321`) and writes:

- A PID file for stopping later
- An env file containing `export ANTHROPIC_BASE_URL=http://127.0.0.1:54321`

**Source the environment and run Claude Code:**

```bash
source /tmp/companion.env
claude
```

**Stop the companion:**

```bash
majordomo-companion stop --pid-file /tmp/companion.pid
```

---

## What Gets Tracked

### Every Request (`llm_requests`)

Both approaches log every API call with:

- Model name, provider
- Input and output token counts
- Calculated cost
- Request timing
- Custom metadata from `X-Majordomo-*` headers

### Claude Code Details (`claude_request_details`)

Both approaches automatically parse Claude Code request/response pairs to extract:

| Field | Description |
|-------|-------------|
| `message_count` | Total messages in the conversation |
| `user_message_count` | Number of user messages |
| `assistant_message_count` | Number of assistant messages |
| `tool_names` | Tools used in this request (e.g., `{Read,Edit,Bash}`) |
| `tool_use_count` | Number of tool calls in the response |
| `has_thinking` | Whether extended thinking was active |
| `is_plan_mode` | Whether Claude Code was in plan mode (read-only tools only) |
| `stop_reason` | Why the response ended (`end_turn`, `tool_use`, `max_tokens`) |
| `system_prompt_hash` | SHA256 hash of the system prompt |

### Sessions (`claude_sessions`) — Companion Only

The companion groups requests into sessions with running aggregates:

| Field | Description |
|-------|-------------|
| `id` | Session ID (UUID) |
| `started_at` / `ended_at` | Session duration |
| `total_requests` | Number of API calls in this session |
| `total_input_tokens` | Sum of input tokens |
| `total_output_tokens` | Sum of output tokens |
| `total_cost` | Cumulative cost |

---

## Useful Queries

### Cost Per Developer (Using Metadata)

```sql
SELECT
    raw_metadata->>'Developer' as developer,
    COUNT(*) as requests,
    SUM(total_cost) as total_cost
FROM llm_requests
WHERE raw_metadata->>'Developer' IS NOT NULL
GROUP BY 1
ORDER BY total_cost DESC;
```

### Cost Per Developer (Using API Keys)

If using per-developer Majordomo API keys:

```sql
SELECT
    ak.name as developer,
    COUNT(*) as requests,
    SUM(lr.total_cost) as total_cost
FROM llm_requests lr
JOIN api_keys ak ON lr.majordomo_api_key_id = ak.id
GROUP BY ak.id, ak.name
ORDER BY total_cost DESC;
```

### Model Usage Breakdown

```sql
SELECT
    model,
    COUNT(*) as requests,
    SUM(input_tokens) as input_tokens,
    SUM(output_tokens) as output_tokens,
    SUM(total_cost) as total_cost
FROM llm_requests lr
JOIN claude_request_details crd ON crd.llm_request_id = lr.id
GROUP BY model
ORDER BY total_cost DESC;
```

### Tool Usage

```sql
SELECT
    unnest(tool_names) as tool,
    COUNT(*) as times_used
FROM claude_request_details
GROUP BY 1
ORDER BY times_used DESC;
```

### Thinking vs Non-Thinking Cost

```sql
SELECT
    has_thinking,
    COUNT(*) as requests,
    SUM(lr.total_cost) as total_cost
FROM claude_request_details crd
JOIN llm_requests lr ON crd.llm_request_id = lr.id
GROUP BY has_thinking;
```

### Plan Mode vs Full Mode

```sql
SELECT
    CASE WHEN is_plan_mode THEN 'Plan Mode' ELSE 'Full Mode' END as mode,
    COUNT(*) as requests,
    SUM(lr.total_cost) as total_cost
FROM claude_request_details crd
JOIN llm_requests lr ON crd.llm_request_id = lr.id
GROUP BY is_plan_mode;
```

### Daily Spend

```sql
SELECT
    DATE(requested_at) as date,
    COUNT(*) as requests,
    SUM(total_cost) as total_cost
FROM llm_requests lr
JOIN claude_request_details crd ON crd.llm_request_id = lr.id
GROUP BY 1
ORDER BY date DESC
LIMIT 30;
```

### Cost Per Project

```sql
SELECT
    raw_metadata->>'Project' as project,
    COUNT(*) as requests,
    SUM(total_cost) as total_cost
FROM llm_requests
WHERE raw_metadata->>'Project' IS NOT NULL
GROUP BY 1
ORDER BY total_cost DESC;
```

### Session Summary (Companion Only)

```sql
SELECT
    cs.id,
    ak.name as api_key_name,
    cs.started_at,
    cs.ended_at,
    cs.total_requests,
    cs.total_input_tokens,
    cs.total_output_tokens,
    cs.total_cost
FROM claude_sessions cs
JOIN api_keys ak ON cs.majordomo_api_key_id = ak.id
ORDER BY cs.started_at DESC
LIMIT 20;
```

### Requests in a Session (Companion Only)

```sql
SELECT
    lr.model,
    lr.input_tokens,
    lr.output_tokens,
    lr.total_cost,
    crd.tool_names,
    crd.tool_use_count,
    crd.has_thinking,
    crd.is_plan_mode,
    crd.stop_reason,
    lr.requested_at
FROM claude_request_details crd
JOIN llm_requests lr ON crd.llm_request_id = lr.id
WHERE crd.session_id = 'your-session-uuid-here'
ORDER BY lr.requested_at;
```

---

## Sessions API

The gateway exposes REST endpoints for managing Claude Code sessions. These are used by the companion automatically, but you can also query them directly.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/claude-sessions` | Start a new session |
| `GET` | `/api/v1/claude-sessions` | List sessions (paginated) |
| `GET` | `/api/v1/claude-sessions/{id}` | Get session details |
| `POST` | `/api/v1/claude-sessions/{id}/end` | End a session |
| `GET` | `/api/v1/claude-sessions/{id}/requests` | List requests in a session |

All endpoints require the `X-Majordomo-Key` header.

```bash
# List recent sessions
curl -H "X-Majordomo-Key: mdm_sk_your_key_here" \
  http://localhost:7680/api/v1/claude-sessions

# Get session details
curl -H "X-Majordomo-Key: mdm_sk_your_key_here" \
  http://localhost:7680/api/v1/claude-sessions/your-session-uuid
```

---

## Team Deployment

### Per-Developer API Keys

Create individual Majordomo API keys for each developer:

```bash
./bin/majordomo keys create --name "Alice"
./bin/majordomo keys create --name "Bob"
./bin/majordomo keys create --name "Charlie"
```

Each developer configures their own key — either in `settings.json` (simple setup) or passed to the companion.

### Shared Key with Developer Metadata

Use a single API key and identify developers via metadata headers:

**Simple setup:**
```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:7680",
    "ANTHROPIC_CUSTOM_HEADERS": "X-Majordomo-Key: mdm_sk_team_key\nX-Majordomo-Developer: alice"
  }
}
```

**Companion setup:**
```bash
majordomo-companion exec \
  --gateway http://localhost:7680 \
  --api-key mdm_sk_team_key \
  --metadata Developer=alice \
  -- claude
```

### Shell Alias (Companion)

Add a convenience alias to your shell profile:

```bash
alias mcc='majordomo-companion exec --gateway https://majordomo.yourcompany.com --api-key $MAJORDOMO_API_KEY --'
```

Then just run:

```bash
mcc claude
```

---

## Companion CLI Reference

```
Usage: majordomo-companion <command> [options]

Commands:
  start    Start the companion proxy (runs in foreground)
  stop     Stop a running companion proxy
  exec     Start the proxy, run a command, then stop on exit

Common Options (start, exec):
  --gateway    Gateway URL (default: http://localhost:7680)
  --api-key    Majordomo API key (required)
  --port       Local port to listen on (default: 0 = random)
  --workdir    Directory to read .majordomo/metadata.json from
  --metadata   Metadata key=value pair (repeatable)

Start Options:
  --pid-file   Path to write PID file (required)
  --env-file   Path to write ANTHROPIC_BASE_URL export

Stop Options:
  --pid-file   Path to PID file (required)
```

---

## Troubleshooting

### Requests not appearing in logs

**Simple setup:**

1. Verify the gateway is running: `curl http://localhost:7680/health`
2. Check `ANTHROPIC_BASE_URL` is set: `echo $ANTHROPIC_BASE_URL`
3. Confirm the Majordomo key is in your custom headers

**Companion setup:**

1. Verify the gateway is running: `curl http://localhost:7680/health`
2. Check the API key is valid: `./bin/majordomo keys list`
3. Try registering a session manually:
   ```bash
   curl -X POST -H "X-Majordomo-Key: mdm_sk_your_key_here" \
     http://localhost:7680/api/v1/claude-sessions
   ```

### `claude_request_details` rows missing

The gateway only parses Anthropic Messages API responses with HTTP status < 400. If requests are failing upstream, details won't be recorded. Check that requests appear in `llm_requests` first.

### Metadata not showing up

- `settings.json`: Verify headers are newline-separated (`\n`) in the `ANTHROPIC_CUSTOM_HEADERS` value
- Companion: Check `.majordomo/metadata.json` is valid JSON with string values, and `--metadata` flags use `key=value` format
- Query `raw_metadata` on `llm_requests` to see what was actually captured

### SSL/TLS errors with remote gateway

Make sure the URL includes `https://`:

```bash
export ANTHROPIC_BASE_URL=https://majordomo.yourcompany.com
```

---

## Next Steps

- **Custom metadata indexing**: Configure [active metadata keys](getting-started.md#adding-custom-metadata) for faster queries on high-cardinality fields
- **Proxy keys**: Use [Proxy Keys](proxy-keys.md) to manage Anthropic API keys centrally so developers don't need their own
- **S3 body storage**: Enable [S3 storage](getting-started.md#s3-body-storage) to capture full request/response bodies for debugging
- **Production deployment**: See the [Deployment guide](deployment.md) for Docker Compose, Kubernetes, and standalone Docker options
