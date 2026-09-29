package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// shrinkStreamTimeouts points the header and idle timeouts at
// test-sized values and restores them when the test ends.
func shrinkStreamTimeouts(t *testing.T, header, idle time.Duration) {
	t.Helper()
	oh, oi := responseHeaderTimeout, streamIdleTimeout
	responseHeaderTimeout, streamIdleTimeout = header, idle
	t.Cleanup(func() {
		responseHeaderTimeout, streamIdleTimeout = oh, oi
	})
}

// hangingServer accepts requests and holds each handler open until
// release is closed — a provider that never answers. Closing the
// server alone cannot end the test: the header-timeout error path
// never touches the response body, so the client keeps the TCP
// connections open and srv.Close would block forever waiting for the
// blocked handlers. The release channel is the one lifecycle owner,
// closed by t.Cleanup; then Close has no live handlers to wait for.
func hangingServer(t *testing.T) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv
}

// A provider that accepts the connection but never sends headers must
// fail fast via ResponseHeaderTimeout, not wedge the agent loop.
func TestStreamChatHeaderTimeout(t *testing.T) {
	shrinkStreamTimeouts(t, 100*time.Millisecond, 100*time.Millisecond)
	srv := hangingServer(t)

	start := time.Now()
	p := NewOpenAICompat(srv.URL, "k")
	if _, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"}); err == nil {
		t.Error("openai client: expected a header-timeout error")
	}
	a := NewAnthropic(srv.URL, "k")
	if _, err := a.StreamChat(context.Background(), ChatRequest{Model: "m"}); err == nil {
		t.Error("anthropic client: expected a header-timeout error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s, want both failures within ~1s", elapsed)
	}
}

// stallServer sends the given SSE events, then holds the stream open —
// a provider whose stream stalls mid-answer.
func stallServer(events ...string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
}

// collectWithDeadline reads the event channel but gives up after
// deadline, so a broken watchdog fails the test in seconds instead of
// hanging until the go test timeout.
func collectWithDeadline(t *testing.T, events <-chan ChatEvent, deadline time.Duration) []ChatEvent {
	t.Helper()
	done := make(chan []ChatEvent, 1)
	go func() { done <- collect(t, events) }()
	select {
	case got := <-done:
		return got
	case <-time.After(deadline):
		t.Fatal("consume never returned — the idle watchdog did not fire")
		return nil
	}
}

// A stream that stalls after its first event must surface a readable
// error once the idle timeout lapses, on both clients.
func TestStreamChatIdleWatchdog(t *testing.T) {
	shrinkStreamTimeouts(t, time.Second, 100*time.Millisecond)

	// OpenAI-compatible: one text delta, then nothing.
	sse := stallServer(textDelta("partial"))
	p := NewOpenAICompat(sse.URL, "k")
	events, err := p.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	got := collectWithDeadline(t, events, 5*time.Second)
	sse.Close()
	assertStalled(t, got)

	// Anthropic Messages API: the same stall, the same watchdog. A real
	// stream opens each block with content_block_start; consume ignores
	// deltas for blocks it never saw start, so the fixture must too.
	msg := stallServer(
		`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`,
	)
	a := NewAnthropic(msg.URL, "k")
	events, err = a.StreamChat(context.Background(), ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	got = collectWithDeadline(t, events, 5*time.Second)
	msg.Close()
	assertStalled(t, got)
}

// assertStalled checks the stream delivered the pre-stall text and
// ended with a readable watchdog error.
func assertStalled(t *testing.T, got []ChatEvent) {
	t.Helper()
	if len(got) < 2 || got[0].Type != TextEvent || got[0].Text != "partial" {
		t.Errorf("events = %+v, want the pre-stall text first", got)
		return
	}
	last := got[len(got)-1]
	if last.Type != ErrorEvent || last.Err == nil ||
		!strings.Contains(last.Err.Error(), "provider stalled") {
		t.Errorf("last event = %+v, want a stalled-stream error", last)
	}
}

// chunkBody is a fake response body: Read returns queued chunks and
// otherwise blocks until the request context is canceled, like a real
// connection under a canceled request.
type chunkBody struct {
	ctx    context.Context
	chunks chan []byte
}

func (c *chunkBody) Read(p []byte) (int, error) {
	select {
	case b := <-c.chunks:
		return copy(p, b), nil
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	}
}

func (c *chunkBody) Close() error { return nil }

func newTestIdleBody(t *testing.T, idle time.Duration) (*idleBody, *chunkBody, context.Context) {
	t.Helper()
	shrinkStreamTimeouts(t, time.Second, idle)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cb := &chunkBody{ctx: ctx, chunks: make(chan []byte, 1)}
	return newIdleBody(cb, cancel), cb, ctx
}

// Time the consumer spends away from Read (startup, or draining a
// full events channel) is not provider silence and must not trip the
// watchdog.
func TestIdleBodySlowConsumerNotKilled(t *testing.T) {
	b, cb, ctx := newTestIdleBody(t, 50*time.Millisecond)
	buf := make([]byte, 8)

	cb.chunks <- []byte("a")
	if _, err := b.Read(buf); err != nil {
		t.Fatalf("first read: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // consumer busy, well past the idle window
	cb.chunks <- []byte("b")
	if _, err := b.Read(buf); err != nil {
		t.Fatalf("read after slow consumer: %v", err)
	}
	if ctx.Err() != nil {
		t.Error("context canceled although the provider never went silent")
	}
}

// A long stream with steady data, total time far beyond the idle
// window, must survive.
func TestIdleBodySteadyStreamNotKilled(t *testing.T) {
	b, cb, ctx := newTestIdleBody(t, 100*time.Millisecond)
	buf := make([]byte, 8)

	for i := 0; i < 15; i++ { // ~450ms total vs a 100ms window
		go func() {
			time.Sleep(30 * time.Millisecond)
			cb.chunks <- []byte("x")
		}()
		if _, err := b.Read(buf); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}
	if ctx.Err() != nil {
		t.Error("steady stream was canceled")
	}
}

// A Read that waits past the idle window fails with the readable
// watchdog error, not a bare "context canceled".
func TestIdleBodyStallReportsWatchdog(t *testing.T) {
	b, _, ctx := newTestIdleBody(t, 50*time.Millisecond)

	_, err := b.Read(make([]byte, 8))
	if err == nil || !strings.Contains(err.Error(), "provider stalled") {
		t.Fatalf("err = %v, want a provider-stalled error", err)
	}
	if ctx.Err() == nil {
		t.Error("stall did not cancel the request context")
	}
}

// Close must release the request context even when nothing was ever
// read, and be safe to call twice.
func TestIdleBodyCloseReleasesContext(t *testing.T) {
	b, _, ctx := newTestIdleBody(t, time.Minute)

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if ctx.Err() == nil {
		t.Error("Close left the request context live")
	}
	if err := b.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// Close arriving while a Read is blocked (a consumer giving up) must
// not race with the Read.
func TestIdleBodyCloseDuringRead(t *testing.T) {
	b, _, _ := newTestIdleBody(t, time.Minute)

	done := make(chan error, 1)
	go func() {
		_, err := b.Read(make([]byte, 8))
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	_ = b.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Error("blocked Read returned nil after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Read was not released by Close")
	}
}

// Model listing is a bounded request: a provider that never answers
// must fail within the (shrunken) models timeout.
func TestFetchModelsTimeout(t *testing.T) {
	old := modelsTimeout
	modelsTimeout = 50 * time.Millisecond
	t.Cleanup(func() { modelsTimeout = old })
	srv := hangingServer(t)

	start := time.Now()
	if _, err := FetchModels(context.Background(), "openai", srv.URL, ""); err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s, want failure within ~1s", elapsed)
	}
}
