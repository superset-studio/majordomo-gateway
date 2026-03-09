package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

type usageRequest struct {
	Preset          string           `json:"preset"`
	Start           string           `json:"start"`
	End             string           `json:"end"`
	APIKeyID        string           `json:"api_key_id"`
	MetadataFilters []metadataFilter `json:"metadata_filters"`
	Limit           int              `json:"limit"`
	Offset          int              `json:"offset"`
}

type metadataFilter struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// decodeUsageRequest decodes a JSON body into a UsageFilter.
func decodeUsageRequest(r *http.Request) (*storage.UsageFilter, *usageRequest, error) {
	var req usageRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, nil, fmt.Errorf("invalid JSON body: %w", err)
		}
	}

	if len(req.MetadataFilters) > 2 {
		return nil, nil, fmt.Errorf("at most 2 metadata filters allowed")
	}

	filter := &storage.UsageFilter{}

	// Parse API key filter
	if req.APIKeyID != "" {
		parsed, err := uuid.Parse(req.APIKeyID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid api_key_id: %w", err)
		}
		filter.APIKeyID = &parsed
	}

	// Parse metadata filters
	for _, mf := range req.MetadataFilters {
		if mf.Key == "" || mf.Value == "" {
			return nil, nil, fmt.Errorf("metadata filter key and value must be non-empty")
		}
		filter.MetadataFilters = append(filter.MetadataFilters, storage.MetadataFilter{
			Key:   mf.Key,
			Value: mf.Value,
		})
	}

	// Parse date range
	if req.Start != "" {
		start, err := parseDate(req.Start)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid start date: %w", err)
		}
		filter.Start = start

		if req.End != "" {
			end, err := parseDate(req.End)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end date: %w", err)
			}
			filter.End = end
		} else {
			filter.End = time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
		}
	} else {
		// Use preset (default: 30d)
		preset := req.Preset
		if preset == "" {
			preset = "30d"
		}

		now := time.Now().UTC()
		filter.End = now.Truncate(24 * time.Hour).Add(24 * time.Hour)

		switch preset {
		case "7d":
			filter.Start = filter.End.AddDate(0, 0, -7)
		case "90d":
			filter.Start = filter.End.AddDate(0, 0, -90)
		default:
			filter.Start = filter.End.AddDate(0, 0, -30)
		}
	}

	return filter, &req, nil
}

// setFilterScope sets the ownership scope on a UsageFilter based on JWT claims.
// If the user belongs to an org, queries are scoped to the org; otherwise to the user.
func setFilterScope(filter *storage.UsageFilter, claims *auth.JWTClaims) {
	if claims.OrgID != nil {
		filter.OrgID = claims.OrgID
	} else {
		filter.UserID = claims.UserID
	}
}

// parseDate parses YYYY-MM-DD or RFC3339.
func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
