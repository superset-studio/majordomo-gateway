# Tool Call Logging

## Background

The majordomo gateway already captures what every LLM request costs and how long it took. But for teams building with tool-using models, cost and latency tell an incomplete story. The question that matters is: what did the model actually do?

Modern LLMs — Claude, GPT-4o, Gemini — respond not just with text but with tool invocations: database queries, file reads, web searches, API calls, code execution. These actions are the primary mechanism through which AI agents affect the world. Today, they are invisible in the gateway logs. A request that triggered three tool calls looks identical to one that triggered none.

## Problem

When an LLM invokes a tool, that invocation exists only inside the response body. It is parsed by the client application and acted upon — but the gateway, sitting between the client and the provider, discards it. The result is a gap between what was billed (tokens, cost) and what actually happened (actions taken).

This gap creates three concrete problems:

**Debugging is hard.** When an agent misbehaves, engineers have no record of which tools it used and in what order. Reproducing the failure means reconstructing it from application logs, if those logs exist at all.

**Security reviews are blind.** Teams selling to enterprise customers are increasingly asked to demonstrate what systems their AI is connected to. Without tool call logs, the answer is "we don't know" — which is not an acceptable answer in a procurement review.

**Usage patterns are opaque.** Product teams cannot answer basic questions: which tools do our agents use most? Which tools appear in failed sessions? Is our agent using the right tools, or falling back to suboptimal ones?

## What We're Building

The gateway will parse tool invocations out of every successful LLM response and persist them as structured log records, associated with the originating request.

This requires no changes to how clients or agents integrate. The gateway reads the response body it already captures, extracts tool call data, and writes it asynchronously so request latency is unaffected.

### What Gets Logged

For each tool invocation in a response:

- **Tool name** — the name of the function or tool the model invoked
- **Call index** — the position of this invocation within the response (models can invoke multiple tools in a single reply)
- **Request association** — the `llm_request` record this invocation belongs to

This is intentionally scoped to invocation identity, not invocation content. Tool inputs and outputs can be large and sensitive; logging them is a separate, opt-in concern. The initial feature answers "which tools were called" without storing what was passed to them.

### Provider Coverage

Tool call formats differ by provider. The gateway will handle both formats transparently:

- **Anthropic** — `tool_use` content blocks in the response
- **OpenAI / Gemini-OpenAI** — `tool_calls` array on choice messages

Responses without tool calls are ignored — no record is written, no overhead is incurred.

### How It Surfaces

Tool call records are queryable through the same interfaces as request logs: the admin UI, the usage API, and direct database access. The primary views are:

- Per-request: which tools did this request invoke, in what order
- Per-key: which tools does this API key's workload use, and how often
- Aggregate: which tools appear most frequently across all traffic

## What This Enables

Once tool calls are logged, several downstream capabilities become straightforward:

- **Agent traces** — grouping a sequence of requests into a single agent run, with the full tool call sequence visible across steps
- **Tool-based policies** — blocking requests that attempt to invoke disallowed tools before they reach the model
- **Anomaly detection** — flagging agents that invoke unexpected tools or invoke the same tool an abnormal number of times

None of these require changes to the core logging schema. They layer on top of the tool call records.

## Non-Goals

This feature does not log tool inputs, outputs, or execution results — only that an invocation occurred and which tool was named. It does not modify, intercept, or block tool calls. It does not require any changes to client code, agent frameworks, or provider API keys.
