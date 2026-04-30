package api

import (
	"net/http"

	"github.com/superset-studio/majordomo-gateway/internal/httputil"
)

// AdminOnlyMiddleware restricts access to a fixed set of usernames.
// Used to gate "operator" admin endpoints (programmatic user/key
// management) that the regular signed-in users should not be able to
// reach. Empty allowlist disables the endpoints entirely.
//
// Must be composed AFTER JWTAuthMiddleware so that GetUserInfo returns
// the authenticated claims.
func AdminOnlyMiddleware(allowedUsernames []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedUsernames))
	for _, u := range allowedUsernames {
		if u != "" {
			allowed[u] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(allowed) == 0 {
				httputil.WriteJSONError(w, http.StatusForbidden, "admin endpoints disabled")
				return
			}
			claims := GetUserInfo(r.Context())
			if claims == nil {
				httputil.WriteJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if _, ok := allowed[claims.Username]; !ok {
				httputil.WriteJSONError(w, http.StatusForbidden, "admin only")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
