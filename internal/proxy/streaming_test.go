package proxy

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestStreamingSSE_ChunksArriveIncrementally verifies that SSE chunks from the
// upstream are relayed to the client as they arrive — not buffered until the
// stream ends. The test sets up a fake upstream that sends chunks with delays,
// then asserts that the client receives early chunks before the upstream has
// finished sending.
func TestStreamingSSE_ChunksArriveIncrementally(t *testing.T) {
	chunks := []string{
		"data: {\"type\":\"content_block_start\"}\n\n",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\n",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\" world\"}}\n\n",
		"data: {\"type\":\"message_stop\"}\n\n",
	}

	chunkDelay := 100 * time.Millisecond

	// Fake upstream that sends SSE chunks with delays.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("upstream ResponseWriter does not support Flush")
			return
		}

		for _, chunk := range chunks {
			_, _ = w.Write([]byte(chunk))
			flusher.Flush()
			time.Sleep(chunkDelay)
		}
	}))
	defer upstream.Close()

	client := NewUpstreamClient(10*time.Second, 10*time.Second)

	// Build a request pointing at the fake upstream.
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))

	streamResp, err := client.ForwardStream(req.Context(), upstream.URL, req, []byte(`{}`))
	if err != nil {
		t.Fatalf("ForwardStream failed: %v", err)
	}
	defer streamResp.Body.Close()

	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", streamResp.StatusCode)
	}

	ct := streamResp.Headers.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream content type, got %q", ct)
	}

	// Read each SSE event from the stream and verify they arrive with timing
	// that proves incremental delivery (not all at once at the end).
	scanner := bufio.NewScanner(streamResp.Body)
	scanner.Split(splitSSEEvents)

	var received []time.Time
	for scanner.Scan() {
		received = append(received, time.Now())
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning SSE events: %v", err)
	}

	if len(received) != len(chunks) {
		t.Fatalf("expected %d events, got %d", len(chunks), len(received))
	}

	// The gap between the first and last event should be at least
	// (len(chunks)-1) * chunkDelay * 0.5 — meaning we're receiving
	// incrementally rather than everything arriving in one burst.
	totalSpan := received[len(received)-1].Sub(received[0])
	minExpected := time.Duration(len(chunks)-1) * chunkDelay / 2
	if totalSpan < minExpected {
		t.Errorf("events arrived too quickly (total span %v, expected at least %v); "+
			"streaming may be buffered", totalSpan, minExpected)
	}
}

// TestStreamingSSE_TeeCapture verifies that the tee-stream pattern captures
// the full response body for logging while simultaneously streaming to the
// client. This simulates what handler.go does.
func TestStreamingSSE_TeeCapture(t *testing.T) {
	ssePayload := "data: {\"type\":\"content_block_start\"}\n\n" +
		"data: {\"type\":\"content_block_delta\"}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, line := range strings.SplitAfter(ssePayload, "\n\n") {
			if line == "" {
				continue
			}
			w.Write([]byte(line))
			flusher.Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer upstream.Close()

	client := NewUpstreamClient(10*time.Second, 10*time.Second)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	streamResp, err := client.ForwardStream(req.Context(), upstream.URL, req, []byte(`{}`))
	if err != nil {
		t.Fatalf("ForwardStream failed: %v", err)
	}

	// Simulate the tee-stream pattern from handler.go:
	// client sees the stream; buf captures the full body.
	var buf bytes.Buffer
	tee := io.TeeReader(streamResp.Body, &buf)

	recorder := httptest.NewRecorder()
	fw := newFlushWriter(recorder)
	_, err = io.Copy(fw, tee)
	streamResp.Body.Close()

	if err != nil {
		t.Fatalf("io.Copy through tee: %v", err)
	}

	// The buffer should have the complete SSE payload.
	if buf.String() != ssePayload {
		t.Errorf("captured body mismatch.\nexpected:\n%s\ngot:\n%s", ssePayload, buf.String())
	}

	// The recorder (client) should also have the complete payload.
	if recorder.Body.String() != ssePayload {
		t.Errorf("client body mismatch.\nexpected:\n%s\ngot:\n%s", ssePayload, recorder.Body.String())
	}
}

// TestStreamingSSE_NonSSEFallsBackToBuffer verifies that non-SSE responses
// from ForwardStream are correctly buffered (the handler reads the full body).
func TestStreamingSSE_NonSSEFallsBackToBuffer(t *testing.T) {
	jsonBody := `{"id":"msg_123","content":[{"text":"Hi"}]}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(jsonBody))
	}))
	defer upstream.Close()

	client := NewUpstreamClient(10*time.Second, 10*time.Second)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	streamResp, err := client.ForwardStream(req.Context(), upstream.URL, req, []byte(`{}`))
	if err != nil {
		t.Fatalf("ForwardStream failed: %v", err)
	}

	ct := streamResp.Headers.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		t.Fatal("expected non-SSE content type")
	}

	// Handler would buffer the body for non-SSE responses.
	body, err := io.ReadAll(streamResp.Body)
	streamResp.Body.Close()
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	if string(body) != jsonBody {
		t.Errorf("expected %q, got %q", jsonBody, string(body))
	}
}

// TestStreamingSSE_WriteDeadlineDisabled verifies that the streaming path
// can handle responses longer than a typical write timeout.
// We use a short server write timeout and a stream that exceeds it.
func TestStreamingSSE_WriteDeadlineDisabled(t *testing.T) {
	numChunks := 5
	chunkDelay := 80 * time.Millisecond // total ~400ms of streaming

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		for i := 0; i < numChunks; i++ {
			w.Write([]byte("data: {\"chunk\":" + string(rune('0'+i)) + "}\n\n"))
			flusher.Flush()
			time.Sleep(chunkDelay)
		}
	}))
	defer upstream.Close()

	// Create a test server with a very short WriteTimeout.
	// Without disabling the write deadline, this would kill the stream.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client := NewUpstreamClient(10*time.Second, 10*time.Second)
		upstreamReq := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

		streamResp, err := client.ForwardStream(upstreamReq.Context(), upstream.URL, upstreamReq, []byte(`{}`))
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}

		// Disable write deadline like handler.go does.
		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Time{})

		w.Header().Set("Content-Type", streamResp.Headers.Get("Content-Type"))
		w.WriteHeader(streamResp.StatusCode)

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		fw := newFlushWriter(w)
		io.Copy(fw, streamResp.Body)
		streamResp.Body.Close()
	})

	// Server with 200ms WriteTimeout — stream takes ~400ms.
	srv := httptest.NewUnstartedServer(handler)
	srv.Config.WriteTimeout = 200 * time.Millisecond
	srv.Start()
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}

	// Count how many chunks we received.
	eventCount := strings.Count(string(body), "data: ")
	if eventCount != numChunks {
		t.Errorf("expected %d chunks, got %d (body: %q)", numChunks, eventCount, string(body))
	}
}

// TestStreamingSSE_FlushWriterFlushesPerWrite verifies that flushWriter
// calls Flush after every Write.
func TestStreamingSSE_FlushWriterFlushesPerWrite(t *testing.T) {
	var mu sync.Mutex
	flushCount := 0
	writeCount := 0

	mock := &mockFlusherWriter{
		onWrite: func(p []byte) (int, error) {
			mu.Lock()
			writeCount++
			mu.Unlock()
			return len(p), nil
		},
		onFlush: func() {
			mu.Lock()
			flushCount++
			mu.Unlock()
		},
	}

	fw := newFlushWriter(mock)

	for i := 0; i < 5; i++ {
		fw.Write([]byte("data: chunk\n\n"))
	}

	mu.Lock()
	defer mu.Unlock()
	if flushCount != writeCount {
		t.Errorf("expected flush count (%d) to match write count (%d)", flushCount, writeCount)
	}
	if writeCount != 5 {
		t.Errorf("expected 5 writes, got %d", writeCount)
	}
}

// --- helpers ---

// splitSSEEvents is a bufio.SplitFunc that splits on double-newline boundaries.
func splitSSEEvents(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		return i + 2, data[:i+2], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// mockFlusherWriter implements http.ResponseWriter and http.Flusher for unit tests.
type mockFlusherWriter struct {
	onWrite func([]byte) (int, error)
	onFlush func()
}

func (m *mockFlusherWriter) Header() http.Header        { return http.Header{} }
func (m *mockFlusherWriter) WriteHeader(statusCode int)  {}
func (m *mockFlusherWriter) Write(p []byte) (int, error) { return m.onWrite(p) }
func (m *mockFlusherWriter) Flush()                      { m.onFlush() }
