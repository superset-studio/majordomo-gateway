package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// hopByHopHeaders are headers that should not be forwarded between client and upstream.
// These are connection-specific and must be handled at each hop.
var hopByHopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailers":            true,
	"transfer-encoding":   true,
	"upgrade":             true,
	"host":                true,
	"content-length":      true, // Let framework recalculate
	"accept-encoding":     true, // Let Go's transport handle compression
}

type UpstreamClient struct {
	httpClient *http.Client
}

func NewUpstreamClient() *UpstreamClient {
	return &UpstreamClient{
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type UpstreamResponse struct {
	StatusCode   int
	Headers      http.Header
	Body         []byte
	ResponseTime time.Duration
}

func (c *UpstreamClient) Forward(ctx context.Context, baseURL string, req *http.Request, body []byte) (*UpstreamResponse, error) {
	start := time.Now()

	targetURL := baseURL + req.URL.Path
	if req.URL.RawQuery != "" {
		targetURL += "?" + req.URL.RawQuery
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	copyHeaders(req.Header, upstreamReq.Header)

	resp, err := c.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return &UpstreamResponse{
		StatusCode:   resp.StatusCode,
		Headers:      resp.Header,
		Body:         respBody,
		ResponseTime: time.Since(start),
	}, nil
}

func copyHeaders(src, dst http.Header) {
	for key, values := range src {
		lowerKey := strings.ToLower(key)
		// Skip majordomo-specific headers
		if strings.HasPrefix(lowerKey, "x-majordomo") {
			continue
		}
		// Skip hop-by-hop headers
		if hopByHopHeaders[lowerKey] {
			continue
		}
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}

// copyResponseHeaders copies headers from upstream response, filtering out
// hop-by-hop headers and Content-Encoding (since Go auto-decompresses).
func copyResponseHeaders(src, dst http.Header) {
	for key, values := range src {
		lowerKey := strings.ToLower(key)
		// Skip hop-by-hop headers
		if hopByHopHeaders[lowerKey] {
			continue
		}
		// Skip Content-Encoding since Go's http client auto-decompresses
		if lowerKey == "content-encoding" {
			continue
		}
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}
