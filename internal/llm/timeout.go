package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// Timeouts are package vars, not consts, so tests can shrink them and
// run in milliseconds instead of minutes.
var (
	// responseHeaderTimeout: how long the provider has to send response
	// headers after we POST. A server that accepts the connection but
	// never answers is dead, and a minute is far beyond any real
	// time-to-first-header.
	responseHeaderTimeout = 60 * time.Second

	// streamIdleTimeout: the longest acceptable gap between bytes on an
	// SSE stream. Healthy providers emit deltas or keep-alive comments;
	// total silence means the stream is dead and the user should hear
	// about it instead of waiting forever.
	streamIdleTimeout = 5 * time.Minute

	// modelsTimeout bounds the model listing — one bounded request,
	// not a stream, so a total timeout is safe here.
	modelsTimeout = 30 * time.Second
)

// newStreamHTTPClient builds the HTTP client both chat clients share.
// Only ResponseHeaderTimeout is set: http.Client.Timeout would also cut
// legitimate long streams, so a stalled stream is caught by the idle
// watchdog (idleBody) instead.
func newStreamHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return &http.Client{Transport: transport}
}

// idleBody wraps an SSE response body with a watchdog: the timer runs
// only while a Read is blocked waiting for the provider, and when that
// wait exceeds streamIdleTimeout the request context is canceled,
// unblocking the Read. Time spent outside Read — consumer startup, or
// a consumer slow to drain the events channel — is not the provider's
// silence and must never count, or a healthy stream would be killed.
// Both chat clients read their streams through this.
type idleBody struct {
	rc      io.ReadCloser
	cancel  context.CancelFunc
	idle    time.Duration
	timer   *time.Timer // built once, disarmed; only Reset/Stop afterwards
	stalled atomic.Bool
}

func newIdleBody(rc io.ReadCloser, cancel context.CancelFunc) *idleBody {
	b := &idleBody{rc: rc, cancel: cancel, idle: streamIdleTimeout}
	// Built here, disarmed, so the field never changes afterwards and
	// Close may race with Read safely (Timer methods are goroutine-safe).
	b.timer = time.AfterFunc(b.idle, func() {
		// Stalled is set before cancel so the unblocked Read already
		// sees it and can report the watchdog, not a bare context error.
		b.stalled.Store(true)
		b.cancel()
	})
	b.timer.Stop()
	return b
}

func (b *idleBody) Read(p []byte) (int, error) {
	b.timer.Reset(b.idle)
	n, err := b.rc.Read(p)
	b.timer.Stop()
	if err != nil && b.stalled.Load() {
		return n, fmt.Errorf("provider stalled — no data for %s", b.idle)
	}
	return n, err
}

// Close stops the watchdog and releases the request context along with
// the body, so the derived context never leaks on any exit path. It is
// safe on a body that was never read (an immediate cancellation) and
// safe to call more than once.
func (b *idleBody) Close() error {
	b.timer.Stop()
	b.cancel()
	return b.rc.Close()
}
