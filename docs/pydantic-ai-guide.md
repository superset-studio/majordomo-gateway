# Pydantic AI + Majordomo Gateway Guide

This guide shows how to integrate [Pydantic AI](https://ai.pydantic.dev/) with Majordomo Gateway for centralized LLM logging and cost tracking. It covers both fresh setups and migrating existing Pydantic AI applications.

## Table of Contents

- [Quick Start](#quick-start)
- [How It Works](#how-it-works)
- [Migration Guide](#migration-guide)
  - [From Direct Provider Access](#from-direct-provider-access)
  - [From Another Proxy](#from-another-proxy)
  - [Keeping Your Model Settings](#keeping-your-model-settings)
- [Provider-Specific Examples](#provider-specific-examples)
  - [Anthropic](#anthropic)
  - [OpenAI](#openai)
  - [Gemini](#gemini)
- [Advanced Usage](#advanced-usage)
  - [Extended Thinking](#extended-thinking)
  - [Prompt Caching](#prompt-caching)
  - [Per-User Cost Tracking](#per-user-cost-tracking)
  - [Multi-Step Workflows](#multi-step-workflows)
- [Environment Variables](#environment-variables)
- [Troubleshooting](#troubleshooting)

---

## Quick Start

### 1. Install

```bash
pip install majordomo-frameworks[pydantic-ai]
```

### 2. Set Environment Variables

```bash
export MAJORDOMO_GATEWAY_URL=http://localhost:7680
export MAJORDOMO_API_KEY=mdm_sk_...
export ANTHROPIC_API_KEY=sk-ant-...  # or OPENAI_API_KEY, GEMINI_API_KEY
```

### 3. Use with Your Agent

```python
from pydantic_ai import Agent
from pydantic_ai.settings import AnthropicModelSettings
from majordomo_frameworks.pydantic_ai import create_model, build_extra_headers

# Create model routed through the gateway
model = create_model("anthropic")

# Build Majordomo headers for tracking
headers = build_extra_headers(feature="my-agent", step="main")

# Create your agent as usual
agent = Agent(model=model, system_prompt="You are a helpful assistant.")

# Run with headers in model settings
result = await agent.run(
    "Hello!",
    model_settings=AnthropicModelSettings(extra_headers=headers),
)
```

---

## How It Works

Majordomo Gateway acts as a transparent proxy between your application and LLM providers:

```
Your App → Majordomo Gateway → Anthropic/OpenAI/Gemini
                  ↓
            PostgreSQL (logs)
```

The gateway:
1. Forwards requests to the upstream provider
2. Logs token usage, costs, and metadata to PostgreSQL
3. Returns responses unchanged

**What changes in your code:**
- Model `base_url` points to the gateway instead of the provider directly
- You add `X-Majordomo-*` headers for tracking (via `extra_headers` in model settings)

**What stays the same:**
- All Pydantic AI features (agents, tools, structured output, streaming)
- All model settings (thinking, caching, timeouts, etc.)
- Your existing agent logic

---

## Migration Guide

### From Direct Provider Access

If you're currently calling providers directly without any proxy:

**Before:**
```python
from pydantic_ai import Agent
from pydantic_ai.models.anthropic import AnthropicModel
from pydantic_ai.settings import AnthropicModelSettings

model = AnthropicModel("claude-sonnet-4-20250514")
agent = Agent(model=model, system_prompt="...")

result = await agent.run(
    "Hello!",
    model_settings=AnthropicModelSettings(
        max_tokens=4096,
        anthropic_thinking={"type": "enabled", "budget_tokens": 10000},
    ),
)
```

**After:**
```python
from pydantic_ai import Agent
from pydantic_ai.settings import AnthropicModelSettings
from majordomo_frameworks.pydantic_ai import create_model, build_extra_headers

# Only change: use create_model() instead of AnthropicModel()
model = create_model("anthropic", "claude-sonnet-4-20250514")
agent = Agent(model=model, system_prompt="...")

# Merge Majordomo headers into your existing settings
majordomo_headers = build_extra_headers(feature="my-agent")

result = await agent.run(
    "Hello!",
    model_settings=AnthropicModelSettings(
        max_tokens=4096,
        anthropic_thinking={"type": "enabled", "budget_tokens": 10000},
        extra_headers=majordomo_headers,  # Add this line
    ),
)
```

**Summary of changes:**
1. Replace `AnthropicModel(...)` with `create_model("anthropic", ...)`
2. Add `extra_headers=build_extra_headers(...)` to your model settings

### From Another Proxy

If you're already using a proxy (like a corporate gateway or LiteLLM):

**Before:**
```python
from pydantic_ai.models.anthropic import AnthropicModel
from pydantic_ai.providers.anthropic import AnthropicProvider
from pydantic_ai.settings import AnthropicModelSettings

model = AnthropicModel(
    "claude-sonnet-4-20250514",
    provider=AnthropicProvider(
        base_url="https://your-current-proxy.example.com",
        api_key=os.environ["API_KEY"],
    ),
)

settings = AnthropicModelSettings(
    max_tokens=64000,
    extra_headers={"X-Custom-Header": "value"},  # Your existing headers
)
```

**After:**
```python
from pydantic_ai.settings import AnthropicModelSettings
from majordomo_frameworks.pydantic_ai import create_model, build_extra_headers

# create_model() handles base_url and api_key for you
model = create_model("anthropic", "claude-sonnet-4-20250514")

# Merge your existing headers with Majordomo headers
majordomo_headers = build_extra_headers(feature="my-agent")
my_headers = {"X-Custom-Header": "value", **majordomo_headers}

settings = AnthropicModelSettings(
    max_tokens=64000,
    extra_headers=my_headers,
)
```

**Key points:**
- `create_model()` reads `MAJORDOMO_GATEWAY_URL` and provider API keys from environment
- Merge your existing `extra_headers` with the Majordomo headers dict
- All other settings remain unchanged

### Keeping Your Model Settings

The migration is designed to preserve all your existing model settings. Here's a complete example showing that everything works together:

```python
from pydantic_ai import Agent
from pydantic_ai.settings import AnthropicModelSettings
from majordomo_frameworks.pydantic_ai import create_model, build_extra_headers

model = create_model("anthropic", "claude-opus-4-5-20251101")
agent = Agent(model=model, system_prompt="You are a research assistant.")

# All your existing settings still work
settings = AnthropicModelSettings(
    # Token limits
    max_tokens=64000,

    # Extended thinking
    anthropic_thinking={"type": "enabled", "budget_tokens": 32000},

    # Prompt caching
    anthropic_cache_instructions=True,
    anthropic_cache_tool_definitions="1h",
    anthropic_cache_messages=True,

    # Tool settings
    parallel_tool_calls=True,

    # Timeout for long requests
    timeout=10 * 60,

    # Majordomo tracking headers (just add this)
    extra_headers=build_extra_headers(
        feature="research-agent",
        step="analysis",
        user_id="user-123",
    ),
)

result = await agent.run("Analyze this data...", model_settings=settings)
```

---

## Provider-Specific Examples

### Anthropic

```python
from pydantic_ai import Agent
from pydantic_ai.settings import AnthropicModelSettings
from majordomo_frameworks.pydantic_ai import create_model, build_extra_headers

model = create_model("anthropic")  # defaults to claude-sonnet-4-20250514
agent = Agent(model=model, system_prompt="You are helpful.")

result = await agent.run(
    "Hello!",
    model_settings=AnthropicModelSettings(
        extra_headers=build_extra_headers(feature="chat"),
    ),
)
```

### OpenAI

```python
from pydantic_ai import Agent
from pydantic_ai.settings import OpenAIChatModelSettings
from majordomo_frameworks.pydantic_ai import create_model, build_extra_headers

model = create_model("openai")  # defaults to gpt-4o
agent = Agent(model=model, system_prompt="You are helpful.")

result = await agent.run(
    "Hello!",
    model_settings=OpenAIChatModelSettings(
        extra_headers=build_extra_headers(feature="chat"),
    ),
)
```

### Gemini

Gemini uses the OpenAI-compatible endpoint, so it needs a special header to tell the gateway where to route:

```python
from pydantic_ai import Agent
from pydantic_ai.settings import OpenAIChatModelSettings
from majordomo_frameworks.pydantic_ai import create_model, build_extra_headers_gemini

model = create_model("gemini")  # defaults to gemini-2.0-flash
agent = Agent(model=model, system_prompt="You are helpful.")

# Use build_extra_headers_gemini() instead of build_extra_headers()
result = await agent.run(
    "Hello!",
    model_settings=OpenAIChatModelSettings(
        extra_headers=build_extra_headers_gemini(feature="chat"),
    ),
)
```

---

## Advanced Usage

### Extended Thinking

Extended thinking works exactly as before—just add Majordomo headers:

```python
from pydantic_ai.settings import AnthropicModelSettings
from majordomo_frameworks.pydantic_ai import create_model, build_extra_headers

model = create_model("anthropic", "claude-opus-4-5-20251101")
agent = Agent(model=model, system_prompt="Think step by step.")

result = await agent.run(
    "Solve this complex problem...",
    model_settings=AnthropicModelSettings(
        max_tokens=64000,
        anthropic_thinking={"type": "enabled", "budget_tokens": 32000},
        extra_headers={
            "anthropic-beta": "interleaved-thinking-2025-05-14",
            **build_extra_headers(feature="reasoning"),
        },
    ),
)
```

### Prompt Caching

Anthropic's prompt caching is fully supported:

```python
settings = AnthropicModelSettings(
    anthropic_cache_instructions=True,      # Cache system prompt
    anthropic_cache_tool_definitions="1h",  # Cache tools for 1 hour
    anthropic_cache_messages=True,          # Cache conversation history
    extra_headers=build_extra_headers(feature="cached-agent"),
)
```

### Per-User Cost Tracking

Track costs per user for billing or analytics:

```python
# Headers include user_id for attribution
headers = build_extra_headers(
    feature="chat-bot",
    user_id=current_user.id,
    session_id=session.id,
)

result = await agent.run(message, model_settings=AnthropicModelSettings(
    extra_headers=headers,
))
```

Query costs per user:
```sql
SELECT
    raw_metadata->>'User-Id' as user_id,
    COUNT(*) as requests,
    SUM(total_cost) as total_cost
FROM llm_requests
WHERE raw_metadata->>'Feature' = 'chat-bot'
GROUP BY 1
ORDER BY total_cost DESC;
```

### Multi-Step Workflows

Use the `step` parameter to track costs across workflow stages:

```python
async def research_workflow(topic: str):
    # Step 1: Generate queries
    result1 = await query_agent.run(
        topic,
        model_settings=AnthropicModelSettings(
            extra_headers=build_extra_headers(feature="research", step="query-gen"),
        ),
    )

    # Step 2: Synthesize results
    result2 = await synthesis_agent.run(
        results,
        model_settings=AnthropicModelSettings(
            extra_headers=build_extra_headers(feature="research", step="synthesis"),
        ),
    )

    return result2
```

Query costs by step:
```sql
SELECT
    raw_metadata->>'Step' as step,
    AVG(total_cost) as avg_cost,
    SUM(total_cost) as total_cost
FROM llm_requests
WHERE raw_metadata->>'Feature' = 'research'
GROUP BY 1;
```

---

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `MAJORDOMO_GATEWAY_URL` | No | Gateway URL (default: `http://localhost:7680`) |
| `MAJORDOMO_API_KEY` | Yes | Your Majordomo API key |
| `OPENAI_API_KEY` | For OpenAI | OpenAI API key |
| `ANTHROPIC_API_KEY` | For Anthropic | Anthropic API key |
| `GEMINI_API_KEY` | For Gemini | Gemini API key |

---

## Troubleshooting

### "MAJORDOMO_API_KEY environment variable is required"

Set your Majordomo API key:
```bash
export MAJORDOMO_API_KEY=mdm_sk_...
```

### Requests not appearing in logs

1. Verify the gateway is running: `curl http://localhost:7680/health`
2. Check that `MAJORDOMO_GATEWAY_URL` is set correctly
3. Ensure you're passing `extra_headers` in your model settings

### "Connection refused" errors

The gateway isn't running or the URL is wrong:
```bash
# Check gateway status
curl http://localhost:7680/health

# Or check your MAJORDOMO_GATEWAY_URL
echo $MAJORDOMO_GATEWAY_URL
```

### Gemini requests failing

Make sure you're using `build_extra_headers_gemini()` (not `build_extra_headers()`):
```python
# Correct for Gemini
headers = build_extra_headers_gemini(feature="my-feature")

# Wrong for Gemini (missing X-Majordomo-Provider header)
headers = build_extra_headers(feature="my-feature")
```

### Headers not being sent

Make sure you're passing `model_settings` to `agent.run()`:
```python
# Correct
result = await agent.run("prompt", model_settings=settings)

# Wrong - headers won't be sent
result = await agent.run("prompt")
```

---

## Quick Reference

```python
from pydantic_ai import Agent
from pydantic_ai.settings import AnthropicModelSettings
from majordomo_frameworks.pydantic_ai import create_model, build_extra_headers

# 1. Create model (handles base_url)
model = create_model("anthropic", "claude-sonnet-4-20250514")

# 2. Create agent
agent = Agent(model=model, system_prompt="...")

# 3. Build headers
headers = build_extra_headers(
    feature="feature-name",    # Required: groups costs
    step="step-name",          # Optional: workflow stage
    user_id="user-id",         # Optional: per-user tracking
    session_id="session-id",   # Optional: conversation tracking
)

# 4. Run with settings
result = await agent.run(
    "prompt",
    model_settings=AnthropicModelSettings(
        extra_headers=headers,
        # ... your other settings
    ),
)
```
