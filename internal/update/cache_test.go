package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNotice(t *testing.T) {
	cases := []struct {
		name    string
		current string
		cached  string
		want    string // "" means no notice
	}{
		{"newer available", "v1.0.0", "v1.1.0", "Update available: v1.0.0 → v1.1.0. Run `tilde update` to install it."},
		{"up to date", "v1.1.0", "v1.1.0", ""},
		{"running newer", "v1.2.0", "v1.1.0", ""},
		{"no cache", "v1.0.0", "", ""},
		{"dev build", "(devel)", "v1.1.0", ""},
		{"pseudo-version", "v1.1.0-0.20250101000000-abcdef123456", "v1.2.0", ""},
		{"bad cached tag", "v1.0.0", "latest", ""},
		{"prerelease to release", "v1.1.0-rc.1", "v1.1.0", "Update available: v1.1.0-rc.1 → v1.1.0. Run `tilde update` to install it."},
		{"prerelease to prerelease", "v1.1.0-rc.1", "v1.1.0-rc.2", "Update available: v1.1.0-rc.1 → v1.1.0-rc.2. Run `tilde update` to install it."},
	}
	for _, c := range cases {
		if got := Notice(c.current, c.cached); got != c.want {
			t.Errorf("%s: Notice(%q, %q) = %q, want %q", c.name, c.current, c.cached, got, c.want)
		}
	}
}

// TestPendingIsTheNoticeGate: the tag the footer badge and --version
// show comes from Pending, so it must name exactly the release the
// startup note names — and nothing else.
func TestPendingIsTheNoticeGate(t *testing.T) {
	cases := []struct{ current, cached, want string }{
		{"v1.0.0", "v1.1.0", "v1.1.0"},
		{"v1.0.0", "v1.1.0-rc.1", "v1.1.0-rc.1"},
		{"v1.0.0-rc.1", "v1.0.0", "v1.0.0"},
		{"v1.1.0", "v1.1.0", ""},
		{"v1.2.0", "v1.1.0", ""},
		{"v1.0.0", "", ""},
		{"(devel)", "v9.9.9", ""},
		{"v1.0.0", "latest", ""},
		{"v1.0.0", "v1.0.1+dirty", ""},
	}
	for _, c := range cases {
		got := Pending(c.current, c.cached)
		if got != c.want {
			t.Errorf("Pending(%q, %q) = %q, want %q", c.current, c.cached, got, c.want)
		}
		note := Notice(c.current, c.cached)
		if (got != "") != (note != "") {
			t.Errorf("Pending(%q, %q) = %q disagrees with the notice %q", c.current, c.cached, got, note)
			continue
		}
		if got != "" && !strings.Contains(note, got) {
			t.Errorf("notice %q does not name the pending tag %q", note, got)
		}
	}
}

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := CachePath(dir)
	if got := filepath.Base(path); got != ".update-cache.json" {
		t.Errorf("CachePath base = %q, want .update-cache.json", got)
	}
	if _, ok := ReadCache(path); ok {
		t.Error("ReadCache on a missing file must report unchecked")
	}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	WriteCache(path, Cache{Tag: "v1.2.0", CheckedAt: now})
	c, ok := ReadCache(path)
	if !ok || c.Tag != "v1.2.0" || !c.CheckedAt.Equal(now) || c.Err != "" {
		t.Errorf("round trip = %+v %v, want v1.2.0 at %v with no error", c, ok, now)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("cache mode = %v, want 0600", fi.Mode())
	}
	// A recorded failure round-trips too: it is what keeps a dead
	// network from being retried on every launch.
	WriteCache(path, Cache{Tag: "v1.2.0", CheckedAt: now, Err: "dial tcp: i/o timeout"})
	if c, ok := ReadCache(path); !ok || c.Err != "dial tcp: i/o timeout" || c.Tag != "v1.2.0" {
		t.Errorf("failure round trip = %+v %v", c, ok)
	}
	// Corrupt files read as unchecked, never fatal.
	os.WriteFile(path, []byte("{not json"), 0o600)
	if _, ok := ReadCache(path); ok {
		t.Error("corrupt cache must read as unchecked")
	}
	os.WriteFile(path, []byte(`{"tag":"v9.9.9"}`+"\n"), 0o600)
	if _, ok := ReadCache(path); ok {
		t.Error("a cache with no timestamp must read as unchecked")
	}
	// A damaged or restored-from-somewhere-else file must not be
	// read into memory at launch: the read is capped.
	big := `{"tag":"v9.9.9","checked_at":"2026-09-29T12:00:00Z","error":"` +
		strings.Repeat("x", maxCacheBytes) + `"}`
	os.WriteFile(path, []byte(big), 0o600)
	if _, ok := ReadCache(path); ok {
		t.Error("an oversized cache must read as unchecked, not as a record")
	}
}

func TestWriteCacheCapsTheError(t *testing.T) {
	path := CachePath(t.TempDir())
	WriteCache(path, Cache{CheckedAt: time.Now(), Err: strings.Repeat("é", 4*maxErrRunes)})
	c, ok := ReadCache(path)
	if !ok {
		t.Fatal("the capped record must still be readable")
	}
	if n := len([]rune(c.Err)); n != maxErrRunes {
		t.Errorf("stored error = %d runes, want the %d-rune cap", n, maxErrRunes)
	}
}

func TestStale(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		c    Cache
		want bool
	}{
		{"just written", Cache{CheckedAt: now}, false},
		{"23h old", Cache{CheckedAt: now.Add(-23 * time.Hour)}, false},
		{"25h old", Cache{CheckedAt: now.Add(-25 * time.Hour)}, true},
		// A failure is retried on an hourly schedule: long enough
		// that a dead network costs one attempt, not one per launch.
		{"failure 30m old", Cache{CheckedAt: now.Add(-30 * time.Minute), Err: "offline"}, false},
		{"failure 90m old", Cache{CheckedAt: now.Add(-90 * time.Minute), Err: "offline"}, true},
		{"zero time", Cache{}, true},
	}
	for _, c := range cases {
		if got := Stale(c.c, now); got != c.want {
			t.Errorf("%s: Stale = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestChecksEnabled(t *testing.T) {
	off := false
	on := true
	t.Setenv("TILDE_NO_UPDATE_CHECK", "")
	if !ChecksEnabled(nil) || !ChecksEnabled(&on) {
		t.Error("checks are enabled by default")
	}
	if ChecksEnabled(&off) {
		t.Error("config false opts out")
	}
	t.Setenv("TILDE_NO_UPDATE_CHECK", "1")
	if ChecksEnabled(nil) || ChecksEnabled(&on) {
		t.Error("TILDE_NO_UPDATE_CHECK=1 opts out even when config is true")
	}
}

func TestSupportedPlatform(t *testing.T) {
	if !SupportedPlatform("linux", "amd64") {
		t.Error("linux/amd64 is a prebuilt platform")
	}
	if SupportedPlatform("plan9", "amd64") {
		t.Error("plan9/amd64 has no prebuilt binary")
	}
}

// latestServer serves the releases/latest JSON shape.
func latestServer(tag string, hits *int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name": "` + tag + `"}`))
	}))
}

// brokenServer always fails, the way a captive portal or a dead
// interface does.
func brokenServer(hits *int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		http.Error(w, "no route to host", http.StatusBadGateway)
	}))
}

func TestLatestKnownFetchesWhenStale(t *testing.T) {
	hits := 0
	srv := latestServer("v9.9.9", &hits)
	defer srv.Close()
	home := t.TempDir()
	now := time.Now()
	want := "Update available: v1.0.0 → v9.9.9. Run `tilde update` to install it."
	got := LatestKnown(context.Background(), "v1.0.0", home, srv.URL, now)
	if got.Notice != want {
		t.Errorf("LatestKnown = %q, want %q", got.Notice, want)
	}
	// The tag rides along for the surfaces that show a bare version.
	if got.Tag != "v9.9.9" {
		t.Errorf("Status.Tag = %q, want v9.9.9", got.Tag)
	}
	if hits != 1 {
		t.Errorf("expected exactly one fetch, got %d", hits)
	}
	if c, ok := ReadCache(CachePath(home)); !ok || c.Tag != "v9.9.9" || c.Err != "" {
		t.Errorf("fetch must be cached clean, got %+v %v", c, ok)
	}
	// A fresh cache is reused with no second fetch.
	hits = 0
	if got := LatestKnown(context.Background(), "v1.0.0", home, srv.URL, now.Add(time.Hour)); got.Notice != want {
		t.Errorf("cached LatestKnown = %q, want %q", got.Notice, want)
	}
	if hits != 0 {
		t.Errorf("fresh cache must not refetch, got %d hits", hits)
	}
}

func TestLatestKnownUpToDate(t *testing.T) {
	hits := 0
	srv := latestServer("v1.0.0", &hits)
	defer srv.Close()
	home := t.TempDir()
	got := LatestKnown(context.Background(), "v1.0.0", home, srv.URL, time.Now())
	if got.Notice != "" || got.Tag != "" {
		t.Errorf("an up-to-date binary must report nothing, got %+v", got)
	}
	if hits != 1 {
		t.Errorf("the check still runs, so exactly one fetch is expected; got %d", hits)
	}
}

// TestLatestKnownSilentOnFailure: an unreachable API reads as "no
// notice", and the attempt is recorded so /doctor can say so.
func TestLatestKnownSilentOnFailure(t *testing.T) {
	home := t.TempDir()
	// No server: connection refused must read as "no notice", not an error.
	if got := LatestKnown(context.Background(), "v1.0.0", home, "http://127.0.0.1:1", time.Now()); got != (Status{}) {
		t.Errorf("failed fetch must be silent, got %+v", got)
	}
	c, ok := ReadCache(CachePath(home))
	if !ok || c.Err == "" || c.Tag != "" {
		t.Errorf("the failed attempt must be recorded with no tag, got %+v %v", c, ok)
	}

	// A stale cache survives a failed refresh: the old tag still warns.
	now := time.Now()
	WriteCache(CachePath(home), Cache{Tag: "v2.0.0", CheckedAt: now.Add(-48 * time.Hour)})
	want := "Update available: v1.0.0 → v2.0.0. Run `tilde update` to install it."
	got := LatestKnown(context.Background(), "v1.0.0", home, "http://127.0.0.1:1", now)
	if got.Notice != want || got.Tag != "v2.0.0" {
		t.Errorf("stale cache on failed refresh = %+v, want the %q notice", got, want)
	}
	if c, _ := ReadCache(CachePath(home)); c.Tag != "v2.0.0" || c.Err == "" {
		t.Errorf("the failure must be recorded without losing the known tag, got %+v", c)
	}
}

// TestLatestKnownThrottlesFailures: twenty launches on a dead network
// must not cost twenty network round trips.
func TestLatestKnownThrottlesFailures(t *testing.T) {
	hits := 0
	srv := brokenServer(&hits)
	defer srv.Close()
	home := t.TempDir()
	now := time.Now()

	LatestKnown(context.Background(), "v1.0.0", home, srv.URL, now)
	if hits != 1 {
		t.Fatalf("first launch should call once, got %d", hits)
	}
	for i := 0; i < 20; i++ {
		LatestKnown(context.Background(), "v1.0.0", home, srv.URL, now.Add(time.Duration(i)*time.Minute))
	}
	if hits != 1 {
		t.Errorf("21 launches on a dead network made %d calls, want 1", hits)
	}
	// Past the failure window it tries again, and a working check
	// clears the recorded failure.
	LatestKnown(context.Background(), "v1.0.0", home, srv.URL, now.Add(2*time.Hour))
	if hits != 2 {
		t.Errorf("the retry window should have elapsed; %d calls", hits)
	}

	good := latestServer("v9.9.9", &hits)
	defer good.Close()
	WriteCache(CachePath(home), Cache{Tag: "v1.0.0", CheckedAt: now.Add(-48 * time.Hour), Err: "offline"})
	if got := LatestKnown(context.Background(), "v1.0.0", home, good.URL, now); got.Tag != "v9.9.9" {
		t.Errorf("a working check must report the new tag, got %+v", got)
	}
	if c, _ := ReadCache(CachePath(home)); c.Err != "" {
		t.Errorf("a working check must clear the recorded failure, got %+v", c)
	}
}

func TestLatestKnownSkipsDevBuilds(t *testing.T) {
	hits := 0
	srv := latestServer("v9.9.9", &hits)
	defer srv.Close()
	home := t.TempDir()
	if got := LatestKnown(context.Background(), "(devel)", home, srv.URL, time.Now()); got != (Status{}) {
		t.Errorf("dev builds never check, got %+v", got)
	}
	if hits != 0 {
		t.Errorf("dev builds must not fetch, got %d hits", hits)
	}
	if _, ok := ReadCache(CachePath(home)); ok {
		t.Error("a dev build must not write a cache either")
	}
}

// TestLatestKnownRejectsJunkTag: an upstream tag tilde cannot read is
// a failed attempt, not a version — and it must not buy a day of
// silence under a tag no surface would ever show.
func TestLatestKnownRejectsJunkTag(t *testing.T) {
	hits := 0
	srv := latestServer("nightly", &hits)
	defer srv.Close()
	home := t.TempDir()
	if got := LatestKnown(context.Background(), "v1.0.0", home, srv.URL, time.Now()); got != (Status{}) {
		t.Errorf("a junk tag must produce no notice, got %+v", got)
	}
	c, ok := ReadCache(CachePath(home))
	if !ok || c.Tag != "" {
		t.Fatalf("junk must not be cached as a tag, got %+v %v", c, ok)
	}
	if !strings.Contains(c.Err, "nightly") {
		t.Errorf("the rejected tag must be named in the recorded failure, got %q", c.Err)
	}
}
