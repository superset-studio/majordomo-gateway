-- Majordomo API Keys
CREATE TABLE IF NOT EXISTS api_keys (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash            VARCHAR(64) NOT NULL UNIQUE,
    name                VARCHAR(255) NOT NULL,
    description         TEXT,
    is_active           BOOLEAN NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at          TIMESTAMPTZ,
    last_used_at        TIMESTAMPTZ,
    request_count       BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash) WHERE is_active = true;

-- LLM Request Logs
CREATE TABLE IF NOT EXISTS llm_requests (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Majordomo API key (for validation and tracking)
    majordomo_api_key_id    UUID REFERENCES api_keys(id),

    -- Provider API key (hashed, for usage tracking per provider key)
    provider_api_key_hash   VARCHAR(64),
    provider_api_key_alias  VARCHAR(255),

    provider                VARCHAR(100) NOT NULL,
    model                   VARCHAR(100) NOT NULL,
    request_path            TEXT NOT NULL,
    request_method          TEXT NOT NULL,

    requested_at            TIMESTAMPTZ NOT NULL,
    responded_at            TIMESTAMPTZ NOT NULL,
    response_time_ms        INT NOT NULL,

    input_tokens            INT NOT NULL,
    output_tokens           INT NOT NULL,
    cached_tokens           INT DEFAULT 0,
    cache_creation_tokens   INT DEFAULT 0,

    input_cost              NUMERIC(12, 8) NOT NULL,
    output_cost             NUMERIC(12, 8) NOT NULL,
    total_cost              NUMERIC(12, 8) NOT NULL,

    status_code             INT NOT NULL,
    error_message           TEXT,

    -- All metadata (no index - for data retention)
    raw_metadata            JSONB,
    -- Only active keys (GIN indexed - for analytics queries)
    indexed_metadata        JSONB DEFAULT '{}',

    request_body            TEXT,
    response_body           TEXT,

    created_at              TIMESTAMPTZ DEFAULT now(),
    body_s3_key             TEXT,
    model_alias_found       BOOLEAN NOT NULL DEFAULT true
);

CREATE INDEX IF NOT EXISTS idx_llm_requests_majordomo_key_time ON llm_requests(majordomo_api_key_id, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_requests_provider_key_time ON llm_requests(provider_api_key_hash, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_requests_indexed_metadata_gin ON llm_requests USING GIN (indexed_metadata);

-- Metadata key configuration per Majordomo API key
CREATE TABLE IF NOT EXISTS llm_requests_metadata_keys (
    majordomo_api_key_id    UUID NOT NULL REFERENCES api_keys(id),
    key_name                VARCHAR(255) NOT NULL,
    display_name            VARCHAR(255),
    key_type                VARCHAR(50) DEFAULT 'string',  -- string, number, boolean
    is_required             BOOLEAN DEFAULT false,

    -- Activation
    is_active               BOOLEAN NOT NULL DEFAULT false,
    activated_at            TIMESTAMPTZ,

    -- Statistics (updated by proxy)
    request_count           BIGINT NOT NULL DEFAULT 0,
    last_seen_at            TIMESTAMPTZ,

    -- HyperLogLog state for cardinality estimation (binary, ~12KB)
    hll_state               BYTEA,
    approx_cardinality      INT NOT NULL DEFAULT 0,
    hll_updated_at          TIMESTAMPTZ,

    created_at              TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (majordomo_api_key_id, key_name)
);

CREATE INDEX IF NOT EXISTS idx_llm_requests_metadata_keys_active ON llm_requests_metadata_keys(majordomo_api_key_id) WHERE is_active = true;

-- Proxy Keys
CREATE TABLE IF NOT EXISTS proxy_keys (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash                VARCHAR(64) NOT NULL UNIQUE,
    name                    VARCHAR(255) NOT NULL,
    description             TEXT,
    majordomo_api_key_id    UUID NOT NULL REFERENCES api_keys(id),
    is_active               BOOLEAN NOT NULL DEFAULT true,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at              TIMESTAMPTZ,
    last_used_at            TIMESTAMPTZ,
    request_count           BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_proxy_keys_hash ON proxy_keys(key_hash) WHERE is_active = true;
CREATE INDEX IF NOT EXISTS idx_proxy_keys_majordomo_key ON proxy_keys(majordomo_api_key_id);

-- Proxy Key Provider Mappings
CREATE TABLE IF NOT EXISTS proxy_key_provider_mappings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    proxy_key_id    UUID NOT NULL REFERENCES proxy_keys(id) ON DELETE CASCADE,
    provider        VARCHAR(100) NOT NULL,
    encrypted_key   TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(proxy_key_id, provider)
);

-- Add proxy_key_id to llm_requests
ALTER TABLE llm_requests ADD COLUMN IF NOT EXISTS proxy_key_id UUID REFERENCES proxy_keys(id);

-- Users (for web UI login)
CREATE TABLE IF NOT EXISTS users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username        VARCHAR(255) NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Optional user ownership of API keys
ALTER TABLE users ADD COLUMN IF NOT EXISTS email VARCHAR(255);
ALTER TABLE users ADD COLUMN IF NOT EXISTS auth_provider VARCHAR(50);
ALTER TABLE users ADD COLUMN IF NOT EXISTS auth_provider_id VARCHAR(255);
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_auth_provider ON users(auth_provider, auth_provider_id) WHERE auth_provider IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email IS NOT NULL;

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id) WHERE user_id IS NOT NULL;

-- User ownership on LLM requests (for efficient per-user queries)
ALTER TABLE llm_requests ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id);
CREATE INDEX IF NOT EXISTS idx_llm_requests_user_id_time ON llm_requests(user_id, requested_at DESC) WHERE user_id IS NOT NULL;

-- Claude Code Sessions
CREATE TABLE IF NOT EXISTS claude_sessions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    majordomo_api_key_id    UUID NOT NULL REFERENCES api_keys(id),
    started_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at                TIMESTAMPTZ,
    total_requests          INT NOT NULL DEFAULT 0,
    total_input_tokens      INT NOT NULL DEFAULT 0,
    total_output_tokens     INT NOT NULL DEFAULT 0,
    total_cost              NUMERIC(12,8) NOT NULL DEFAULT 0,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_claude_sessions_api_key_started
    ON claude_sessions(majordomo_api_key_id, started_at DESC);

-- Claude Code Request Details
CREATE TABLE IF NOT EXISTS claude_request_details (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    llm_request_id          UUID NOT NULL UNIQUE REFERENCES llm_requests(id),
    session_id              UUID REFERENCES claude_sessions(id),
    message_count           INT NOT NULL DEFAULT 0,
    user_message_count      INT NOT NULL DEFAULT 0,
    assistant_message_count INT NOT NULL DEFAULT 0,
    tool_names              TEXT[],
    tool_use_count          INT NOT NULL DEFAULT 0,
    has_thinking            BOOLEAN NOT NULL DEFAULT false,
    is_plan_mode            BOOLEAN NOT NULL DEFAULT false,
    stop_reason             VARCHAR(50),
    system_prompt_hash      VARCHAR(64),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_claude_request_details_session
    ON claude_request_details(session_id, created_at) WHERE session_id IS NOT NULL;

-- Per-user S3 body storage configuration
ALTER TABLE users ADD COLUMN IF NOT EXISTS s3_bucket VARCHAR(255);
ALTER TABLE users ADD COLUMN IF NOT EXISTS s3_region VARCHAR(50) DEFAULT 'us-east-1';
ALTER TABLE users ADD COLUMN IF NOT EXISTS s3_endpoint VARCHAR(500);
ALTER TABLE users ADD COLUMN IF NOT EXISTS s3_access_key_id_encrypted TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS s3_secret_access_key_encrypted TEXT;

-- Optional session name for Claude Code sessions
ALTER TABLE claude_sessions ADD COLUMN IF NOT EXISTS session_name VARCHAR(255);

-- Organizations
CREATE TABLE IF NOT EXISTS organizations (
    id                              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                            VARCHAR(255) NOT NULL,
    slug                            VARCHAR(255) NOT NULL UNIQUE,
    s3_bucket                       VARCHAR(255),
    s3_region                       VARCHAR(50) DEFAULT 'us-east-1',
    s3_endpoint                     VARCHAR(500),
    s3_access_key_id_encrypted      TEXT,
    s3_secret_access_key_encrypted  TEXT,
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Organization Members
CREATE TABLE IF NOT EXISTS organization_members (
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        VARCHAR(50) NOT NULL DEFAULT 'member',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_org_members_user ON organization_members(user_id);

-- Organization Invites
CREATE TABLE IF NOT EXISTS organization_invites (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email       VARCHAR(255) NOT NULL,
    role        VARCHAR(50) NOT NULL DEFAULT 'member',
    token       VARCHAR(255) NOT NULL UNIQUE,
    invited_by  UUID NOT NULL REFERENCES users(id),
    expires_at  TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, email)
);
CREATE INDEX IF NOT EXISTS idx_org_invites_token ON organization_invites(token);
CREATE INDEX IF NOT EXISTS idx_org_invites_email ON organization_invites(email);

-- Organization ownership on API keys
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_api_keys_org_id ON api_keys(org_id) WHERE org_id IS NOT NULL;

-- Denormalized org_id on llm_requests for efficient per-org queries
ALTER TABLE llm_requests ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_llm_requests_org_id_time ON llm_requests(org_id, requested_at DESC) WHERE org_id IS NOT NULL;

-- Denormalized org_id on claude_sessions
ALTER TABLE claude_sessions ADD COLUMN IF NOT EXISTS org_id UUID REFERENCES organizations(id);

-- Password reset tokens
CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token       VARCHAR(255) NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_user ON password_reset_tokens(user_id);

-- Email verification
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT false;
-- NOTE: Existing users and OAuth users should be grandfathered:
-- UPDATE users SET email_verified = true WHERE auth_provider IS NOT NULL OR created_at < now();

CREATE TABLE IF NOT EXISTS email_verification_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token       VARCHAR(255) NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_email_verification_tokens_user ON email_verification_tokens(user_id);

-- Provider API Keys (for replay — encrypted credentials per user/org)
CREATE TABLE IF NOT EXISTS provider_api_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID REFERENCES users(id) ON DELETE CASCADE,
    org_id          UUID REFERENCES organizations(id) ON DELETE CASCADE,
    provider        VARCHAR(100) NOT NULL,
    encrypted_key   TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (user_id IS NOT NULL AND org_id IS NULL) OR
        (user_id IS NULL AND org_id IS NOT NULL)
    )
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_provider_api_keys_user_provider ON provider_api_keys(user_id, provider) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_provider_api_keys_org_provider ON provider_api_keys(org_id, provider) WHERE org_id IS NOT NULL;

-- Replay Runs (job queue for model optimization replays)
CREATE TABLE IF NOT EXISTS replay_runs (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id),
    org_id                  UUID REFERENCES organizations(id),

    status                  VARCHAR(20) NOT NULL DEFAULT 'pending',
    error_message           TEXT,

    -- Source filters
    source_api_key_id       UUID REFERENCES api_keys(id),
    source_provider         VARCHAR(100),
    source_model            VARCHAR(100),
    source_start            TIMESTAMPTZ,
    source_end              TIMESTAMPTZ,
    source_metadata         JSONB,
    source_limit            INT NOT NULL DEFAULT 50,

    -- Target model
    target_provider         VARCHAR(100) NOT NULL,
    target_model            VARCHAR(100) NOT NULL,

    -- Judge config
    judge_enabled           BOOLEAN NOT NULL DEFAULT false,
    judge_provider          VARCHAR(100),
    judge_model             VARCHAR(100),

    -- Summary stats (populated on completion)
    total_requests          INT,
    exact_matches           INT,
    judge_equivalent        INT,
    divergent               INT,
    original_total_cost     NUMERIC(12,8),
    replay_total_cost       NUMERIC(12,8),
    original_avg_latency_ms INT,
    replay_avg_latency_ms   INT,

    started_at              TIMESTAMPTZ,
    completed_at            TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_replay_runs_user_status ON replay_runs(user_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_replay_runs_org_status ON replay_runs(org_id, status, created_at DESC) WHERE org_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_replay_runs_pending ON replay_runs(status) WHERE status = 'pending';

-- Replay Results (per-request comparison results)
CREATE TABLE IF NOT EXISTS replay_results (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    replay_run_id           UUID NOT NULL REFERENCES replay_runs(id) ON DELETE CASCADE,
    source_request_id       UUID NOT NULL REFERENCES llm_requests(id),

    original_provider       VARCHAR(100) NOT NULL,
    original_model          VARCHAR(100) NOT NULL,
    original_cost           NUMERIC(12,8) NOT NULL,
    original_latency_ms     INT NOT NULL,
    original_input_tokens   INT NOT NULL,
    original_output_tokens  INT NOT NULL,

    replay_response         TEXT,
    replay_cost             NUMERIC(12,8),
    replay_latency_ms       INT,
    replay_input_tokens     INT,
    replay_output_tokens    INT,

    exact_match             BOOLEAN,
    judge_equivalent        BOOLEAN,
    judge_reason            TEXT,

    error_message           TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_replay_results_run ON replay_results(replay_run_id, created_at);

-- Supported LLM Providers and Models (refreshed by worker from majordomo-llm)
CREATE TABLE IF NOT EXISTS llm_providers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider    VARCHAR(100) NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS llm_models (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id UUID NOT NULL REFERENCES llm_providers(id) ON DELETE CASCADE,
    model       VARCHAR(200) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider_id, model)
);
CREATE INDEX IF NOT EXISTS idx_llm_models_provider ON llm_models(provider_id);

-- ============================================================
-- Eval System
-- ============================================================

-- Eval Sets: Named, reusable collections of logged request IDs
CREATE TABLE IF NOT EXISTS eval_sets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    org_id      UUID REFERENCES organizations(id),
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_eval_sets_user ON eval_sets(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_eval_sets_org ON eval_sets(org_id, created_at DESC) WHERE org_id IS NOT NULL;

-- Eval Set Items: Junction table linking eval sets to llm_requests
CREATE TABLE IF NOT EXISTS eval_set_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    eval_set_id     UUID NOT NULL REFERENCES eval_sets(id) ON DELETE CASCADE,
    request_id      UUID NOT NULL REFERENCES llm_requests(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (eval_set_id, request_id)
);
CREATE INDEX IF NOT EXISTS idx_eval_set_items_set ON eval_set_items(eval_set_id);
CREATE INDEX IF NOT EXISTS idx_eval_set_items_request ON eval_set_items(request_id);

-- Eval Runs: Job queue for eval execution
CREATE TABLE IF NOT EXISTS eval_runs (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id),
    org_id                  UUID REFERENCES organizations(id),
    eval_set_id             UUID NOT NULL REFERENCES eval_sets(id),

    status                  VARCHAR(20) NOT NULL DEFAULT 'pending',
    error_message           TEXT,

    target_provider         VARCHAR(100) NOT NULL,
    target_model            VARCHAR(100) NOT NULL,

    -- Evaluator configuration: JSON array of LLM judge configs
    -- [{"name": "accuracy", "prompt": "...", "provider": "anthropic", "model": "...", "scale_min": 1, "scale_max": 5}]
    evaluators              JSONB NOT NULL DEFAULT '[]',

    -- Summary stats (populated on completion)
    total_requests          INT,
    successful_requests     INT,
    failed_requests         INT,
    original_total_cost     NUMERIC(12,8),
    replay_total_cost       NUMERIC(12,8),
    judge_total_cost        NUMERIC(12,8),
    original_avg_latency_ms INT,
    replay_avg_latency_ms   INT,
    -- Per-evaluator aggregates: [{"name": "accuracy", "avg": 4.2, "min": 1, "max": 5, "count": 50}]
    evaluator_summary       JSONB,

    started_at              TIMESTAMPTZ,
    completed_at            TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_eval_runs_user_status ON eval_runs(user_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_eval_runs_org_status ON eval_runs(org_id, status, created_at DESC) WHERE org_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_eval_runs_pending ON eval_runs(status) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_eval_runs_eval_set ON eval_runs(eval_set_id);

-- Eval Results: Per-request result for an eval run
CREATE TABLE IF NOT EXISTS eval_results (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    eval_run_id             UUID NOT NULL REFERENCES eval_runs(id) ON DELETE CASCADE,
    source_request_id       UUID NOT NULL REFERENCES llm_requests(id),

    original_provider       VARCHAR(100) NOT NULL,
    original_model          VARCHAR(100) NOT NULL,
    original_cost           NUMERIC(12,8) NOT NULL,
    original_latency_ms     INT NOT NULL,
    original_input_tokens   INT NOT NULL,
    original_output_tokens  INT NOT NULL,

    replay_response         TEXT,
    replay_cost             NUMERIC(12,8),
    replay_latency_ms       INT,
    replay_input_tokens     INT,
    replay_output_tokens    INT,

    error_message           TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_eval_results_run ON eval_results(eval_run_id, created_at);

-- Eval Result Scores: Per-evaluator score for each eval result
CREATE TABLE IF NOT EXISTS eval_result_scores (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    eval_result_id  UUID NOT NULL REFERENCES eval_results(id) ON DELETE CASCADE,
    evaluator_name  VARCHAR(100) NOT NULL,
    score           NUMERIC(10,4) NOT NULL,
    reason          TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (eval_result_id, evaluator_name)
);
CREATE INDEX IF NOT EXISTS idx_eval_result_scores_result ON eval_result_scores(eval_result_id);
CREATE INDEX IF NOT EXISTS idx_eval_result_scores_name ON eval_result_scores(evaluator_name);

-- Cloud storage provider support (S3 or GCS) for per-user and per-org body storage
ALTER TABLE users ADD COLUMN IF NOT EXISTS cloud_storage_provider VARCHAR(10);
ALTER TABLE users ADD COLUMN IF NOT EXISTS gcs_bucket VARCHAR(255);
ALTER TABLE users ADD COLUMN IF NOT EXISTS gcs_project_id VARCHAR(255);
ALTER TABLE users ADD COLUMN IF NOT EXISTS gcs_credentials_json_encrypted TEXT;

ALTER TABLE organizations ADD COLUMN IF NOT EXISTS cloud_storage_provider VARCHAR(10);
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS gcs_bucket VARCHAR(255);
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS gcs_project_id VARCHAR(255);
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS gcs_credentials_json_encrypted TEXT;

-- One-time migration (run manually after applying schema):
-- UPDATE users SET cloud_storage_provider = 's3' WHERE s3_bucket IS NOT NULL AND s3_bucket != '';
-- UPDATE organizations SET cloud_storage_provider = 's3' WHERE s3_bucket IS NOT NULL AND s3_bucket != '';

-- Waitlist entries for early access signups
CREATE TABLE IF NOT EXISTS waitlist_entries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      VARCHAR(255) NOT NULL UNIQUE,
    source     VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_waitlist_entries_email ON waitlist_entries (email);

-- ============================================================
-- Experiment Routing (A/B Testing)
-- ============================================================

-- Experiments: A/B test definitions scoped to a user or org
CREATE TABLE IF NOT EXISTS experiments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID REFERENCES users(id),
    org_id            UUID REFERENCES organizations(id),
    api_key_id        UUID REFERENCES api_keys(id),  -- NULL = all keys for this user/org
    name              VARCHAR(255) NOT NULL,
    description       TEXT,
    status            VARCHAR(20) NOT NULL DEFAULT 'draft',  -- draft, active, paused, completed
    sticky            BOOLEAN NOT NULL DEFAULT false,
    sticky_key_header VARCHAR(255),  -- custom header name for sticky identity (optional)
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (user_id IS NOT NULL OR org_id IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS idx_experiments_user_status ON experiments(user_id, status) WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_experiments_org_status ON experiments(org_id, status) WHERE org_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_experiments_api_key ON experiments(api_key_id, status) WHERE api_key_id IS NOT NULL;

-- Experiment Variants: each variant targets a specific provider + model with a relative weight
CREATE TABLE IF NOT EXISTS experiment_variants (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_id   UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    name            VARCHAR(255) NOT NULL,
    provider        VARCHAR(100) NOT NULL,
    model           VARCHAR(200) NOT NULL,
    weight          INT NOT NULL DEFAULT 1,
    is_control      BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (experiment_id, name)
);
CREATE INDEX IF NOT EXISTS idx_experiment_variants_experiment ON experiment_variants(experiment_id);

-- Experiment Assignments: sticky variant assignments for consistent routing
CREATE TABLE IF NOT EXISTS experiment_assignments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_id   UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    variant_id      UUID NOT NULL REFERENCES experiment_variants(id) ON DELETE CASCADE,
    subject_hash    VARCHAR(64) NOT NULL,  -- SHA256 of the sticky identity
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (experiment_id, subject_hash)
);
CREATE INDEX IF NOT EXISTS idx_experiment_assignments_lookup ON experiment_assignments(experiment_id, subject_hash);
