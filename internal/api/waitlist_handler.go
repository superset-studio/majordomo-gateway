package api

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/superset-studio/majordomo-gateway/internal/httputil"
	"github.com/superset-studio/majordomo-gateway/internal/storage"
)

// WaitlistHandler handles waitlist signup requests.
type WaitlistHandler struct {
	waitlist storage.WaitlistStorage
	email    EmailSender
	limiter  *ipRateLimiter
}

// NewWaitlistHandler creates a new WaitlistHandler.
func NewWaitlistHandler(waitlist storage.WaitlistStorage, email EmailSender) *WaitlistHandler {
	return &WaitlistHandler{
		waitlist: waitlist,
		email:    email,
		limiter:  newIPRateLimiter(3, time.Minute),
	}
}

type waitlistRequest struct {
	Email   string `json:"email"`
	Company string `json:"company"` // honeypot — should be empty
}

var emailRegexp = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// JoinWaitlist handles POST /api/v1/admin/waitlist.
func (h *WaitlistHandler) JoinWaitlist(w http.ResponseWriter, r *http.Request) {
	// Rate limit by IP
	ip := clientIP(r)
	if !h.limiter.allow(ip) {
		httputil.WriteJSONError(w, http.StatusTooManyRequests, "too many requests, please try again later")
		return
	}

	var req waitlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Honeypot check — bots fill hidden fields, humans don't
	if req.Company != "" {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	if req.Email == "" || len(req.Email) > 255 || !emailRegexp.MatchString(req.Email) {
		httputil.WriteJSONError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	entry, isNew, err := h.waitlist.CreateWaitlistEntry(r.Context(), req.Email, nil)
	if err != nil {
		slog.Error("failed to create waitlist entry", "error", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Only send welcome email for new signups
	if isNew && h.email != nil && entry != nil {
		go func() {
			if err := h.email.SendWaitlistConfirmation(req.Email); err != nil {
				slog.Error("failed to send waitlist confirmation email", "email", req.Email, "error", err)
			}
		}()
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// clientIP extracts the client IP from the request, checking X-Forwarded-For first.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if parts := strings.SplitN(xff, ",", 2); len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ipRateLimiter is a simple in-memory per-IP rate limiter.
type ipRateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	max      int
	window   time.Duration
}

func newIPRateLimiter(max int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		requests: make(map[string][]time.Time),
		max:      max,
		window:   window,
	}
}

func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	// Prune old entries
	timestamps := l.requests[ip]
	valid := timestamps[:0]
	for _, t := range timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= l.max {
		l.requests[ip] = valid
		return false
	}

	l.requests[ip] = append(valid, now)
	return true
}
