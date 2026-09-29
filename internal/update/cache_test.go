package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	}
	for _, c := range cases {
		if got := Notice(c.current, c.cached); got != c.want {
			t.Errorf("%s: Notice(%q, %q) = %q, want %q", c.name, c.current, c.cached, got, c.want)
		}
	}
}

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := CachePath(dir)
	if got := filepath.Base(path); got != ".update-cache.json" {
		t.Errorf("CachePath base = %q, want .update-cache.json", got)
	}
	if _, _, ok := ReadCache(path); ok {
		t.Error("ReadCache on a missing file must report unchecked")
	}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	WriteCache(path, "v1.2.0", now)
	tag, at, ok := ReadCache(path)
	if !ok || tag != "v1.2.0" || !at.Equal(now) {
		t.Errorf("round trip = %q %v %v, want v1.2.0 %v true", tag, at, ok, now)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("cache mode = %v, want 0600", fi.Mode())
	}
	// Corrupt files read as unchecked, never fatal.
	os.WriteFile(path, []byte("{not json"), 0o600)
	if _, _, ok := ReadCache(path); ok {
		t.Error("corrupt cache must read as unchecked")
	}
	os.WriteFile(path, []byte(`{"tag":"","checked_at":"2026-09-29T12:00:00Z"}`+"\n"), 0o600)
	if _, _, ok := ReadCache(path); ok {
		t.Error("empty-tag cache must read as unchecked")
	}
}

func TestStale(t *testing.T) {
	now := time.Now()
	if Stale(now, now) {
		t.Error("a just-written cache is not stale")
	}
	if !Stale(now.Add(-25*time.Hour), now) {
		t.Error("a 25h-old cache is stale")
	}
	if Stale(now.Add(-23*time.Hour), now) {
		t.Error("a 23h-old cache is not stale yet")
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

func TestLatestKnownFetchesWhenStale(t *testing.T) {
	hits := 0
	srv := latestServer("v9.9.9", &hits)
	defer srv.Close()
	home := t.TempDir()
	now := time.Now()
	got := LatestKnown(context.Background(), "v1.0.0", home, srv.URL, now)
	want := "Update available: v1.0.0 → v9.9.9. Run `tilde update` to install it."
	if got != want {
		t.Errorf("LatestKnown = %q, want %q", got, want)
	}
	if hits != 1 {
		t.Errorf("expected exactly one fetch, got %d", hits)
	}
	if tag, _, ok := ReadCache(CachePath(home)); !ok || tag != "v9.9.9" {
		t.Errorf("fetch must be cached, got %q %v", tag, ok)
	}
	// A fresh cache is reused with no second fetch.
	hits = 0
	if got := LatestKnown(context.Background(), "v1.0.0", home, srv.URL, now.Add(time.Hour)); got != want {
		t.Errorf("cached LatestKnown = %q, want %q", got, want)
	}
	if hits != 0 {
		t.Errorf("fresh cache must not refetch, got %d hits", hits)
	}
}

func TestLatestKnownSilentOnFailure(t *testing.T) {
	home := t.TempDir()
	// No server: connection refused must read as "no notice", not an error.
	if got := LatestKnown(context.Background(), "v1.0.0", home, "http://127.0.0.1:1", time.Now()); got != "" {
		t.Errorf("failed fetch must be silent, got %q", got)
	}
	// A stale cache survives a failed refresh: the old tag still warns.
	now := time.Now()
	WriteCache(CachePath(home), "v2.0.0", now.Add(-48*time.Hour))
	want := "Update available: v1.0.0 → v2.0.0. Run `tilde update` to install it."
	if got := LatestKnown(context.Background(), "v1.0.0", home, "http://127.0.0.1:1", now); got != want {
		t.Errorf("stale cache on failed refresh = %q, want %q", got, want)
	}
}

func TestLatestKnownSkipsDevBuilds(t *testing.T) {
	hits := 0
	srv := latestServer("v9.9.9", &hits)
	defer srv.Close()
	home := t.TempDir()
	if got := LatestKnown(context.Background(), "(devel)", home, srv.URL, time.Now()); got != "" {
		t.Errorf("dev builds never check, got %q", got)
	}
	if hits != 0 {
		t.Errorf("dev builds must not fetch, got %d hits", hits)
	}
}
