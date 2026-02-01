package storage

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

type PostgresStorage struct {
	db             *sqlx.DB
	logChan        chan *models.RequestLog
	done           chan struct{}
	activeKeyCache *ActiveKeysCache
	hllManager     *HLLManager
}

// PostgresStorageConfig holds configuration for the storage layer.
type PostgresStorageConfig struct {
	HLLFlushInterval   time.Duration
	ActiveKeysCacheTTL time.Duration
}

func NewPostgresStorage(ctx context.Context, dsn string, maxConns int, cfg *PostgresStorageConfig) (*PostgresStorage, error) {
	db, err := sqlx.ConnectContext(ctx, "postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(maxConns)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	// Use defaults if config not provided
	if cfg == nil {
		cfg = &PostgresStorageConfig{
			HLLFlushInterval:   60 * time.Second,
			ActiveKeysCacheTTL: 5 * time.Minute,
		}
	}

	s := &PostgresStorage{
		db:             db,
		logChan:        make(chan *models.RequestLog, 1000),
		done:           make(chan struct{}),
		activeKeyCache: NewActiveKeysCache(db, cfg.ActiveKeysCacheTTL),
		hllManager:     NewHLLManager(db, cfg.HLLFlushInterval),
	}

	// Load persisted HLLs on startup
	if err := s.hllManager.LoadFromDB(ctx); err != nil {
		slog.Warn("failed to load HLL state from DB", "error", err)
	}

	s.hllManager.Start()
	go s.writeLoop()

	return s, nil
}

func (s *PostgresStorage) writeLoop() {
	for {
		select {
		case log := <-s.logChan:
			s.writeLog(log)
		case <-s.done:
			for len(s.logChan) > 0 {
				s.writeLog(<-s.logChan)
			}
			return
		}
	}
}

func (s *PostgresStorage) writeLog(log *models.RequestLog) {
	ctx := context.Background()

	// Get active keys from cache
	activeKeys, _ := s.activeKeyCache.GetActiveKeys(ctx, log.APIKeyHash)

	// Split metadata into raw and indexed
	indexedMetadata := make(map[string]string)
	for key, value := range log.RawMetadata {
		if activeKeys[key] {
			indexedMetadata[key] = value
		}
	}

	// Marshal both metadata columns
	rawMetadataJSON, err := json.Marshal(log.RawMetadata)
	if err != nil {
		slog.Error("failed to marshal raw metadata", "error", err)
		rawMetadataJSON = []byte("{}")
	}

	indexedMetadataJSON, err := json.Marshal(indexedMetadata)
	if err != nil {
		slog.Error("failed to marshal indexed metadata", "error", err)
		indexedMetadataJSON = []byte("{}")
	}

	query := `
		INSERT INTO llm_requests (
			id, api_key_hash, api_key_alias, provider, model, request_path, request_method,
			requested_at, responded_at, response_time_ms,
			input_tokens, output_tokens, cached_tokens,
			input_cost, output_cost, total_cost,
			status_code, error_message, raw_metadata, indexed_metadata, request_body, response_body, body_s3_key,
			model_alias_found
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24
		)`

	_, err = s.db.ExecContext(ctx, query,
		log.ID, log.APIKeyHash, log.APIKeyAlias, log.Provider, log.Model, log.RequestPath, log.RequestMethod,
		log.RequestedAt, log.RespondedAt, log.ResponseTimeMs,
		log.InputTokens, log.OutputTokens, log.CachedTokens,
		log.InputCost, log.OutputCost, log.TotalCost,
		log.StatusCode, log.ErrorMessage, rawMetadataJSON, indexedMetadataJSON, log.RequestBody, log.ResponseBody, log.BodyS3Key,
		log.ModelAliasFound,
	)
	if err != nil {
		slog.Error("failed to write request log", "error", err, "request_id", log.ID)
		return
	}

	// Update HLLs for each metadata key/value
	for key, value := range log.RawMetadata {
		s.hllManager.AddValue(log.APIKeyHash, key, value)
	}

	// Register metadata keys (best effort, don't fail if this errors)
	s.registerMetadataKeys(ctx, log.APIKeyHash, log.RawMetadata)
}

func (s *PostgresStorage) registerMetadataKeys(ctx context.Context, apiKeyHash string, metadata map[string]string) {
	if len(metadata) == 0 {
		return
	}

	query := `
		INSERT INTO llm_requests_metadata_keys (api_key_hash, key_name)
		VALUES ($1, $2)
		ON CONFLICT (api_key_hash, key_name) DO NOTHING`

	for key := range metadata {
		_, err := s.db.ExecContext(ctx, query, apiKeyHash, key)
		if err != nil {
			slog.Warn("failed to register metadata key", "error", err, "key", key)
		}
	}
}

func (s *PostgresStorage) WriteRequestLog(ctx context.Context, log *models.RequestLog) {
	select {
	case s.logChan <- log:
	default:
		slog.Warn("request log channel full, dropping log", "request_id", log.ID)
	}
}

func (s *PostgresStorage) Close() error {
	close(s.done)
	// Stop HLL manager (triggers final flush)
	if s.hllManager != nil {
		s.hllManager.Stop()
	}
	return s.db.Close()
}
