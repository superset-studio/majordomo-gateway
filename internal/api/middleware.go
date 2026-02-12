package api

import (
	"context"
	"net/http"

	"github.com/superset-studio/majordomo-gateway/internal/auth"
	"github.com/superset-studio/majordomo-gateway/internal/models"
)

type contextKey string

const apiKeyInfoKey contextKey = "apiKeyInfo"

// AuthMiddleware validates the X-Majordomo-Key header and stores the resolved
// APIKeyInfo in the request context.
func AuthMiddleware(resolver *auth.Resolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-Majordomo-Key")
			info, err := resolver.ResolveAPIKey(r.Context(), apiKey)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), apiKeyInfoKey, info)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetAPIKeyInfo retrieves the authenticated APIKeyInfo from the request context.
func GetAPIKeyInfo(ctx context.Context) *models.APIKeyInfo {
	info, _ := ctx.Value(apiKeyInfoKey).(*models.APIKeyInfo)
	return info
}
