CREATE TABLE IF NOT EXISTS llm_requests (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_key_hash        VARCHAR(2550) NOT NULL,
    api_key_alias       VARCHAR(255) NULL,

    provider            VARCHAR(100) NOT NULL,
    model               VARCHAR(100) NOT NULL,
    request_path        TEXT NOT NULL,
    request_method      TEXT NOT NULL,

    requested_at        TIMESTAMPTZ NOT NULL,
    responded_at        TIMESTAMPTZ NOT NULL,
    response_time_ms    INT NOT NULL,

    input_tokens        INT NOT NULL,
    output_tokens       INT NOT NULL,
    cached_tokens       INT DEFAULT 0,

    input_cost          NUMERIC(12, 8) NOT NULL,
    output_cost         NUMERIC(12, 8) NOT NULL,
    total_cost          NUMERIC(12, 8) NOT NULL,

    status_code         INT NOT NULL,
    error_message       TEXT,

    -- All metadata (no index - for data retention)
    raw_metadata        JSONB,
    -- Only active keys (GIN indexed - for analytics queries)
    indexed_metadata    JSONB DEFAULT '{}',

    request_body        TEXT,
    response_body       TEXT,

    created_at          TIMESTAMPTZ DEFAULT now(),
    body_s3_key         TEXT,
    model_alias_found   BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS llm_requests_metadata_keys (
    api_key_hash VARCHAR(2550) NOT NULL,
    key_name VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    key_type VARCHAR(50) DEFAULT 'string',  -- string, number, boolean
    is_required BOOLEAN DEFAULT false,

    -- Activation
    is_active BOOLEAN NOT NULL DEFAULT false,
    activated_at TIMESTAMPTZ,
    activated_by UUID REFERENCES users(id),

    -- Statistics (updated by proxy)
    request_count BIGINT NOT NULL DEFAULT 0,
    last_seen_at TIMESTAMPTZ,

    -- HyperLogLog state for cardinality estimation (binary, ~12KB)
    hll_state BYTEA,
    approx_cardinality INT NOT NULL DEFAULT 0,
    hll_updated_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (api_key_hash, key_name)
);

CREATE INDEX IF NOT EXISTS idx_llm_requests_api_key_time ON llm_requests(api_key_hash, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_requests_indexed_metadata_gin ON llm_requests USING GIN (indexed_metadata);
CREATE INDEX IF NOT EXISTS idx_llm_requests_metadata_keys_active ON llm_requests_metadata_keys(api_key_hash) WHERE is_active = true;

