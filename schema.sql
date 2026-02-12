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
