// The startup update notice: how a user on a stale binary learns a
// newer release exists without typing anything.
//
// `tilde update` is explicit and stays the only thing that downloads.
// Separately, at startup, main asks LatestKnown (below) whether the
// cached latest-release tag is newer than the running binary and, if
// so, adds one startup note: "Update available: vX -> vY. Run `tilde
// update` to install it." The cache refreshes at most once every 24h
// with a short-timeout HTTPS GET to the releases API (a few KB);
// failures are silent — an offline machine starts exactly as before.
// Opt out with config.json "update_checks": false or
// TILDE_NO_UPDATE_CHECK=1. Dev builds ((devel), pseudo-versions) and
// platforms with no prebuilt binary never check: "newer" has no
// meaning for them.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// cacheTTL bounds the check rate: at most one network call per day,
// however often tilde starts.
const cacheTTL = 24 * time.Hour

// checkTimeout bounds the startup check: the notice must never make
// startup feel slow.
const checkTimeout = 8 * time.Second

// cacheName is the cache file under the user's tilde home. Dot-prefixed
// so it stays out of the way of config.json, auth.json, sessions/.
const cacheName = ".update-cache.json"

type cacheFile struct {
	// Tag is the latest release tag seen ("v1.2.3").
	Tag string `json:"tag"`
	// CheckedAt is when the tag was fetched, UTC RFC3339.
	CheckedAt string `json:"checked_at"`
}

// CachePath returns the update-check cache file for a tilde home dir.
func CachePath(home string) string {
	return filepath.Join(home, cacheName)
}

// ChecksEnabled reports whether the startup notice may run: an
// explicit config false or TILDE_NO_UPDATE_CHECK=1 opts out.
func ChecksEnabled(updateChecks *bool) bool {
	if updateChecks != nil && !*updateChecks {
		return false
	}
	switch os.Getenv("TILDE_NO_UPDATE_CHECK") {
	case "", "0", "false":
		return true
	}
	return false
}

// Notice compares the running version against the cached latest tag.
// It returns the startup-note text when the cache names a strictly
// newer release, or "" when up to date, unknown, or unchecked. It
// never touches the network.
func Notice(current, cachedTag string) string {
	if cachedTag == "" {
		return ""
	}
	cur, ok := parseRelease(current)
	if !ok {
		return ""
	}
	latest, ok := parseRelease(cachedTag)
	if !ok {
		return ""
	}
	if compare(cur, latest) < 0 {
		return fmt.Sprintf("Update available: %s → %s. Run `tilde update` to install it.", current, cachedTag)
	}
	return ""
}

// ReadCache returns the cached tag and check time. Missing, corrupt,
// or empty files read as unchecked, never as an error the caller
// must surface.
func ReadCache(path string) (tag string, checkedAt time.Time, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", time.Time{}, false
	}
	var c cacheFile
	if err := json.Unmarshal(data, &c); err != nil || c.Tag == "" {
		return "", time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, c.CheckedAt)
	if err != nil {
		return "", time.Time{}, false
	}
	return c.Tag, t, true
}

// WriteCache records a freshly fetched tag. Best-effort like the
// history file: a notice cache that cannot be written is a missed
// hint, not a failure.
func WriteCache(path, tag string, now time.Time) {
	data, err := json.Marshal(cacheFile{Tag: tag, CheckedAt: now.UTC().Format(time.RFC3339)})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o600)
}

// Stale reports whether the cache needs a refresh.
func Stale(checkedAt time.Time, now time.Time) bool {
	return now.Sub(checkedAt) >= cacheTTL
}

// FetchLatestTag asks the releases API for the latest tag. It is the
// only network call in the notice path: one small JSON document, no
// download.
func FetchLatestTag(ctx context.Context, apiURL string, client *http.Client) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: checkTimeout, CheckRedirect: refuseHTTPSDowngrade}
	}
	o := Options{APIURL: apiURL, Client: client}.withDefaults()
	return latestTag(ctx, o)
}

// SupportedPlatform reports whether prebuilt releases exist for a
// GOOS/GOARCH pair (the release workflow's matrix).
func SupportedPlatform(goos, goarch string) bool {
	return prebuilt[goos+"/"+goarch]
}

// LatestKnown is the whole startup path in one call: read the cache,
// refresh it when stale (silent on failure), and report the notice
// text. current is the running build version; home is the tilde home
// dir; apiURL is DefaultAPIURL in production. now is injectable for
// tests.
func LatestKnown(ctx context.Context, current, home, apiURL string, now time.Time) string {
	// Dev builds and platforms with no prebuilt binary never check:
	// "newer" has no meaning when nothing could be installed.
	if _, ok := parseRelease(current); !ok {
		return ""
	}
	if now.IsZero() {
		now = time.Now()
	}
	path := CachePath(home)
	tag, checkedAt, ok := ReadCache(path)
	if !ok || Stale(checkedAt, now) {
		fetchCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		defer cancel()
		if fresh, err := FetchLatestTag(fetchCtx, apiURL, nil); err == nil {
			WriteCache(path, fresh, now)
			tag = fresh
		} else if !ok {
			return ""
		}
	}
	return Notice(current, tag)
}
