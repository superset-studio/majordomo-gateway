# API Key Policies

## Background

The majordomo gateway is a logging and cost-attribution layer. Every request that flows through it is recorded — model, tokens, cost, latency, metadata. But recording a request and controlling a request are different things. Today, the gateway does the former and not the latter.

For many teams, logging is enough. But as AI usage grows and agents become more autonomous, the ability to observe alone stops being sufficient. Teams need to be able to say: this key is only allowed to use these models, or this budget cannot be exceeded, or this agent cannot call this tool. Right now, there is no enforcement mechanism.

## Problem

Without per-key controls, a few failure modes keep recurring:

**Budget overruns happen silently.** An agent with a bug, a runaway retry loop, or an unexpectedly large workload can exhaust a team's LLM budget before anyone notices. Usage dashboards show the damage after the fact. Nothing stops it in real time.

**Model governance is unenforced.** Teams declare a policy — "all production traffic uses claude-sonnet, never opus" — but nothing in the infrastructure enforces it. A client misconfiguration or a dependency upgrade can silently change which model is being called. The logs will show it eventually; no one is alerted.

**Blast radius of compromised keys is unlimited.** A leaked or misused API key can make any request the gateway supports — any model, any volume, any cost. Scoping what a key is permitted to do reduces the damage surface.

**Compliance questions go unanswered.** Enterprise procurement teams ask: how do you ensure your AI only uses approved models? How do you cap AI spend? The honest answer today is "we watch the dashboards." That is not a satisfying answer.

## What We're Building

A policy engine that evaluates rules against each incoming request before it is forwarded to the upstream provider. A request that violates a policy is rejected immediately with an HTTP error — it never reaches the LLM. Policies are attached to API keys, so different keys can have different rules.

This turns the gateway from an observation layer into a control layer. The same infrastructure that logs what happened can now prevent things from happening.

### Policy Types

**Model allowlist** — Only the listed models are permitted for this key. Requests for any other model are rejected with a 403. This is the primary enforcement mechanism for model governance.

**Model denylist** — The listed models are blocked for this key. All other models are permitted. Useful for excluding expensive or experimental models from production keys.

**Budget limit** — Requests are blocked once cumulative spend for this key exceeds a threshold within a rolling time window (e.g., $50 in the last 30 days). The limit is evaluated against costs already recorded in the database. When the limit is reached, the key stops working until the window resets.

More policy types — tool restrictions, rate limits per time window — can be added without changing the enforcement architecture. The policy engine evaluates whatever rules are attached to a key.

### Enforcement Behavior

Policies run synchronously in the request path, before any forwarding occurs. If a policy is violated:

- The request is rejected with an appropriate HTTP status (403 for model/tool violations, 429 for budget violations)
- The rejection is logged, including which policy was triggered
- Nothing is billed — no upstream call is made

If no policies are attached to a key, the request proceeds normally. Policies are strictly additive; adding a policy to a key never affects other keys.

### Policy Management

Policies are managed through the same interfaces as API keys: the admin UI, the management API, and the CLI. Each policy on a key is an independent rule. A key with both a model allowlist and a budget limit must satisfy both for a request to proceed.

## What This Enables

Policy enforcement makes the gateway useful in contexts where it previously was not:

**Production safety** — Set a hard budget ceiling on any key running in production. An agent with a bug cannot spend more than the limit, regardless of how many requests it makes.

**Multi-tenant products** — Teams building products on top of LLMs can assign each customer their own API key, with policies scoped to what that customer is permitted to use. Model tier controls and spend limits become a product feature, not just an internal concern.

**Compliance documentation** — Policies create a durable, queryable record of what controls were in place at any point in time. Answering "what models were your agents permitted to use in Q1?" becomes a database query.

## Non-Goals

Policies do not modify requests — they permit or reject them. Policy evaluation does not add latency to passing requests beyond a fast database read. Policies are not a rate-limiting system in the traditional sense (requests per second); they operate on model identity and cumulative spend, not throughput.
