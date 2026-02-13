package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/superset-studio/majordomo-gateway/internal/auth"
)

const jwtUserInfoKey contextKey = "jwtUserInfo"

// JWTAuthMiddleware validates the Bearer token and stores JWTClaims in the request context.
func JWTAuthMiddleware(jwtSvc *auth.JWTService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			claims, err := jwtSvc.ValidateToken(parts[1])
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), jwtUserInfoKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserInfo retrieves the authenticated JWTClaims from the request context.
func GetUserInfo(ctx context.Context) *auth.JWTClaims {
	claims, _ := ctx.Value(jwtUserInfoKey).(*auth.JWTClaims)
	return claims
}
