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

-- Per-user GCS body storage configuration
ALTER TABLE users ADD COLUMN IF NOT EXISTS gcs_bucket VARCHAR(255);
ALTER TABLE users ADD COLUMN IF NOT EXISTS gcs_credentials_json_encrypted TEXT;
