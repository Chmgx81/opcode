package tools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestWebFetchRefusesLoopbackUnderEverySpelling is the SSRF the
// spelling-only filter missed: checkFetchTarget compared the literal
// host byte-for-byte against "localhost", so LOCALHOST, the FQDN root
// dot, the localhost.localdomain alias, and the 127.1 shorthand all
// reached a loopback server the model should never see. The refusal
// now happens at dial time on the address actually connected to, so
// the spelling no longer decides.
func TestWebFetchRefusesLoopbackUnderEverySpelling(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, "SECRET-TOKEN")
	}))
	defer srv.Close()
	_, port, err := splitHostPort(t, srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	// 127.0.0.1 and ::1 spell the same loopback plainly; the rest are
	// the aliases and shorthand forms the old comparison missed.
	for _, host := range []string{
		"127.0.0.1",
		"127.1",
		"localhost",
		"LOCALHOST",
		"LocalHost",
		"localhost.",
		"localhost.localdomain",
		"[::1]",
		"0",
		"2130706433",
		"0177.0.0.1",
		"100.64.0.1",
		"255.255.255.255",
		"169.254.169.254",
		"metadata.google.internal",
	} {
		url := fmt.Sprintf("http://%s:%s/secrets", host, port)
		out, err := (WebFetch{}).Execute(context.Background(), jsonArgs(t, map[string]any{"url": url}))
		if err == nil {
			t.Errorf("%s: fetch succeeded: %q", host, out)
		}
		if strings.Contains(out, "SECRET-TOKEN") {
			t.Errorf("%s: loopback content reached the model: %q", host, out)
		}
	}
	if hits != 0 {
		t.Errorf("the loopback server was reached %d times; every spelling must be refused before the connection", hits)
	}
}

// The allow flag is the user's documented opt-in and must keep working
// for exactly what it is for: fetching a loopback dev server.
func TestWebFetchAllowLocalFetchFlagStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "local dev server")
	}))
	defer srv.Close()

	t.Setenv("OPCODE_ALLOW_LOCAL_FETCH", "1")
	host, port, err := splitHostPort(t, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	// The literal address the test server was given, and the name
	// "localhost" — the two spellings a dev server is actually reached
	// by. "127.1" is deliberately absent: it is a valid IPv4 form that
	// only some resolvers accept, so asserting on it tested the
	// machine's resolver rather than opcode.
	for _, spelling := range []string{host, "localhost"} {
		url := fmt.Sprintf("http://%s:%s/", spelling, port)
		out, err := (WebFetch{}).Execute(context.Background(), jsonArgs(t, map[string]any{"url": url}))
		if err != nil {
			t.Errorf("%s: %v", spelling, err)
			continue
		}
		if !strings.Contains(out, "local dev server") {
			t.Errorf("%s: out = %q", spelling, out)
		}
	}
}

// The flag must open the loopback at every layer that refuses it: the
// literal-host check, and the dial-time check that catches a name
// resolving to 127.0.0.1. A flag honored by only one of them would leave
// a dev server unreachable "at random", depending on the spelling used.
func TestWebFetchAllowLocalFetchDisablesBothChecks(t *testing.T) {
	t.Setenv("OPCODE_ALLOW_LOCAL_FETCH", "1")
	// A name the literal check cannot see as loopback at all, and which
	// therefore only the dial-time check would refuse. It must be
	// allowed, and must be refused with the flag unset.
	if _, err := net.DefaultResolver.LookupHost(context.Background(), "localhost"); err != nil {
		t.Skipf("localhost does not resolve here: %v", err)
	}
	if err := checkFetchTarget("http://localhost:8080/"); err != nil {
		t.Errorf("with the flag set, the literal check still refused: %v", err)
	}
	t.Setenv("OPCODE_ALLOW_LOCAL_FETCH", "")
	if err := checkFetchTarget("http://localhost:8080/"); err == nil {
		t.Error("with the flag unset, the literal check allowed localhost")
	}
}

// A redirect hop is re-validated with the same check, so one approval
// of an innocuous URL must not become a silent hop to loopback. (The
// redirect target here is refused at dial time even when the hop is a
// bare loopback address, which the old spelling check let through.)
func TestWebFetchRedirectToLoopbackRefused(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, "SECRET-TOKEN")
	}))
	defer srv.Close()

	_, port, err := splitHostPort(t, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	// The first hop is public-looking, so it passes the spelling
	// check; only the dial-time bound can stop the second one.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("http://127.0.0.1:%s/keys", port), http.StatusFound)
	}))
	defer redirector.Close()

	out, err := (WebFetch{}).Execute(context.Background(), jsonArgs(t, map[string]any{"url": redirector.URL}))
	if err == nil {
		t.Errorf("the redirect to loopback succeeded: %q", out)
	}
	if strings.Contains(out, "SECRET-TOKEN") || hits != 0 {
		t.Errorf("the redirect reached the loopback server (%d hits): %q", hits, out)
	}
}

// The pre-existing guards must survive: only absolute http(s) URLs are
// fetched, so file:// and scheme-relative spellings stay refused, and
// non-text responses still report their size instead of dumping bytes.
func TestWebFetchKeepsSchemeGuard(t *testing.T) {
	for _, url := range []string{"file:///etc/passwd", "/etc/passwd", "ftp://example.com/x"} {
		if _, err := (WebFetch{}).Execute(context.Background(), jsonArgs(t, map[string]any{"url": url})); err == nil {
			t.Errorf("%s was not refused", url)
		}
	}
}

func TestCheckFetchTargetSpellings(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:8080/":             true,
		"http://LOCALHOST:8080/":             true,
		"http://localhost.:8080/":            true,
		"http://localhost.localdomain/":      true,
		"http://127.0.0.1/":                  true,
		"http://[::1]/":                      true,
		"http://10.1.2.3/":                   true,
		"http://169.254.169.254/latest/meta": true,
		"http://100.64.0.1/":                 true,
		"http://255.255.255.255/":            true,
		"http://224.0.0.1/":                  true,
		"http://240.0.0.1/":                  true,
		"http://93.184.216.34/":              false,
		"http://example.com/":                false,
		"http://metadata.example.com/":       false,
	}
	// http://0/ and the decimal/octal forms (2130706433, 0177.0.0.1)
	// are absent here on purpose: net.ParseIP rejects them, so the
	// spelling check cannot see them and the dial-time check is the
	// only thing standing in the way. They are covered end to end in
	// TestWebFetchRefusesLoopbackUnderEverySpelling.
	for rawURL, wantBlocked := range cases {
		err := checkFetchTarget(rawURL)
		if blocked := err != nil; blocked != wantBlocked {
			t.Errorf("checkFetchTarget(%q) blocked=%v, want %v (%v)", rawURL, blocked, wantBlocked, err)
		}
	}
	// The opt-in disables the check entirely, both at the spelling and
	// at the dial.
	t.Setenv("OPCODE_ALLOW_LOCAL_FETCH", "1")
	for rawURL := range cases {
		if err := checkFetchTarget(rawURL); err != nil {
			t.Errorf("OPCODE_ALLOW_LOCAL_FETCH=1 must allow %q: %v", rawURL, err)
		}
	}
}

// splitHostPort pulls host:port out of an httptest URL.
func splitHostPort(t *testing.T, rawURL string) (host, port string, err error) {
	t.Helper()
	u := strings.TrimPrefix(strings.TrimPrefix(rawURL, "http://"), "https://")
	host, port, ok := strings.Cut(u, ":")
	if !ok {
		t.Fatalf("no port in %s", rawURL)
	}
	if _, err := strconv.Atoi(port); err != nil {
		t.Fatalf("bad port %q", port)
	}
	return host, port, nil
}
