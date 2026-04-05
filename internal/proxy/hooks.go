package proxy

import "github.com/superset-studio/majordomo-gateway/extension"

// Type aliases so internal proxy code can reference these without the extension
// package prefix, while external modules consume them via extension.*.
type PolicyViolation = extension.PolicyViolation
type PolicyContext = extension.PolicyContext
type PolicyEnforcer = extension.PolicyEnforcer
type EnrichmentEvent = extension.EnrichmentEvent
type RequestEnricher = extension.RequestEnricher

// HandlerOption configures optional behavior on a Handler.
type HandlerOption func(*Handler)

// WithPolicyEnforcer attaches a synchronous pre-proxy policy enforcer.
func WithPolicyEnforcer(e extension.PolicyEnforcer) HandlerOption {
	return func(h *Handler) { h.policyEnforcer = e }
}

// WithRequestEnricher attaches an async post-response enricher.
func WithRequestEnricher(e extension.RequestEnricher) HandlerOption {
	return func(h *Handler) { h.requestEnricher = e }
}
