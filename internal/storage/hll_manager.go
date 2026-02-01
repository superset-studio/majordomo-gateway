package storage

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/jmoiron/sqlx"
)

type hllKey struct {
	APIKeyHash string
	KeyName    string
}

type hllEntry struct {
	hll   *hyperloglog.Sketch
	dirty bool  // modified since last flush
	count int64 // request count delta
}

// HLLManager manages HyperLogLog sketches for metadata key cardinality estimation.
// It maintains in-memory HLLs, adds values on each request, and periodically
// flushes to the database.
type HLLManager struct {
	mu            sync.RWMutex
	hlls          map[hllKey]*hllEntry
	db            *sqlx.DB
	flushInterval time.Duration
	done          chan struct{}
	wg            sync.WaitGroup
}

// NewHLLManager creates a new HLL manager with the specified flush interval.
func NewHLLManager(db *sqlx.DB, flushInterval time.Duration) *HLLManager {
	return &HLLManager{
		hlls:          make(map[hllKey]*hllEntry),
		db:            db,
		flushInterval: flushInterval,
		done:          make(chan struct{}),
	}
}

// LoadFromDB loads persisted HLL states on startup.
func (m *HLLManager) LoadFromDB(ctx context.Context) error {
	query := `
		SELECT api_key_hash, key_name, hll_state
		FROM llm_requests_metadata_keys
		WHERE hll_state IS NOT NULL`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()

	loaded := 0
	for rows.Next() {
		var apiKeyHash string
		var keyName string
		var hllBytes []byte

		if err := rows.Scan(&apiKeyHash, &keyName, &hllBytes); err != nil {
			slog.Warn("failed to scan HLL row", "error", err)
			continue
		}

		hll := hyperloglog.New()
		if err := hll.UnmarshalBinary(hllBytes); err != nil {
			slog.Warn("failed to unmarshal HLL", "error", err, "api_key_hash", apiKeyHash[:16], "key", keyName)
			continue
		}

		key := hllKey{APIKeyHash: apiKeyHash, KeyName: keyName}
		m.hlls[key] = &hllEntry{hll: hll, dirty: false, count: 0}
		loaded++
	}

	if loaded > 0 {
		slog.Info("loaded HLL states from database", "count", loaded)
	}

	return rows.Err()
}

// AddValue adds a value to the HLL for a given API key hash/key.
func (m *HLLManager) AddValue(apiKeyHash, keyName, value string) {
	key := hllKey{APIKeyHash: apiKeyHash, KeyName: keyName}

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.hlls[key]
	if !ok {
		entry = &hllEntry{hll: hyperloglog.New(), dirty: true, count: 0}
		m.hlls[key] = entry
	}

	entry.hll.Insert([]byte(value))
	entry.dirty = true
	entry.count++
}

// Start begins the periodic flush goroutine.
func (m *HLLManager) Start() {
	m.wg.Add(1)
	go m.flushLoop()
}

func (m *HLLManager) flushLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.Flush(context.Background()); err != nil {
				slog.Error("failed to flush HLLs", "error", err)
			}
		case <-m.done:
			// Final flush on shutdown
			if err := m.Flush(context.Background()); err != nil {
				slog.Error("failed to flush HLLs on shutdown", "error", err)
			}
			return
		}
	}
}

// Flush persists dirty HLLs to the database.
func (m *HLLManager) Flush(ctx context.Context) error {
	m.mu.Lock()
	toFlush := make(map[hllKey]*hllEntry)
	for k, v := range m.hlls {
		if v.dirty {
			// Copy the entry data for flushing
			toFlush[k] = &hllEntry{
				hll:   v.hll,
				count: v.count,
			}
			// Mark as clean and reset count
			v.dirty = false
			v.count = 0
		}
	}
	m.mu.Unlock()

	if len(toFlush) == 0 {
		return nil
	}

	query := `
		UPDATE llm_requests_metadata_keys
		SET hll_state = $1,
			approx_cardinality = $2,
			request_count = request_count + $3,
			last_seen_at = NOW(),
			hll_updated_at = NOW()
		WHERE api_key_hash = $4 AND key_name = $5`

	flushed := 0
	for key, entry := range toFlush {
		hllBytes, err := entry.hll.MarshalBinary()
		if err != nil {
			slog.Warn("failed to marshal HLL", "error", err, "key", key.KeyName)
			continue
		}
		cardinality := entry.hll.Estimate()

		result, err := m.db.ExecContext(ctx, query,
			hllBytes, cardinality, entry.count,
			key.APIKeyHash, key.KeyName)
		if err != nil {
			slog.Warn("failed to flush HLL", "error", err, "api_key_hash", key.APIKeyHash[:16], "key", key.KeyName)
			continue
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			slog.Debug("no row updated for HLL flush (key may not exist yet)", "api_key_hash", key.APIKeyHash[:16], "key", key.KeyName)
		}
		flushed++
	}

	if flushed > 0 {
		slog.Debug("flushed HLL states", "count", flushed)
	}

	return nil
}

// Stop signals the flush loop to exit and waits for completion.
func (m *HLLManager) Stop() {
	close(m.done)
	m.wg.Wait()
}
