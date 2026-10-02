// The startup update notice: how a user on a stale binary learns a
// newer release exists without typing anything.
//
// `opcode update` is explicit and stays the only thing that downloads.
// Separately, at startup, main asks LatestKnown (below) whether the
// cached latest-release tag is newer than the running binary and, if
// so, adds one startup note: "Update available: vX → vY. Run `opcode
// update` to install it." The cache refreshes at most once every 24h
// with a short-timeout HTTPS GET to the releases API (a few KB).
// Failures are silent — an offline machine starts exactly as before —
// but they are recorded, so /doctor can say the check failed rather
// than guess, and a dead network is retried on an hourly schedule
// instead of on every launch. Opt out with config.json
// "update_checks": false or OPCODE_NO_UPDATE_CHECK=1. Dev builds
// ((devel), pseudo-versions) and platforms with no prebuilt binary
// never check: "newer" has no meaning for them.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// cacheTTL bounds a working check's rate: at most one network call
	// per day, however often opcode starts.
	cacheTTL = 24 * time.Hour
	// failureTTL is the retry window after a failed check. Shorter
	// than cacheTTL, so fixing a network is noticed the same
	// afternoon; long enough that a laptop with no route does not pay
	// the timeout on every launch.
	failureTTL = time.Hour

	// checkTimeout bounds the startup check. The check blocks the
	// first frame, so the budget is a startup budget, not a download
	// budget: three seconds is enough for the releases API on a slow
	// link, and a miss costs one failed attempt, not the session.
	checkTimeout = 3 * time.Second

	// maxCacheBytes bounds the cache read. The file opcode writes is
	// ~150 bytes; anything larger is damaged, not informative, and
	// must not be read into memory at launch.
	maxCacheBytes = 8 << 10
	// maxErrRunes caps the recorded failure text, so a chatty server
	// cannot grow the cache file.
	maxErrRunes = 200
)

// cacheName is the cache file under the user's opcode home. Dot-prefixed
// so it stays out of the way of config.json, auth.json, sessions/.
const cacheName = ".update-cache.json"

type cacheFile struct {
	// Tag is the latest release tag seen ("v1.2.3").
	Tag string `json:"tag"`
	// CheckedAt is when the attempt happened, UTC RFC3339.
	CheckedAt string `json:"checked_at"`
	// Err is why the last attempt failed, "" when it worked. Stored
	// so /doctor can distinguish "offline" from "never ran" instead
	// of reading a missing tag as either.
	Err string `json:"error,omitempty"`
}

// Cache is one update-check attempt's result, as stored on disk. A
// failed attempt keeps the last known Tag: the release it names is
// still real, so the notice keeps working with no network at all.
type Cache struct {
	Tag       string    // the latest release tag seen, "" if never learned
	CheckedAt time.Time // when the attempt happened
	Err       string    // why it failed, "" when it worked
}

// CachePath returns the update-check cache file for a opcode home dir.
func CachePath(home string) string {
	return filepath.Join(home, cacheName)
}

// ChecksEnabled reports whether the startup notice may run: an
// explicit config false or OPCODE_NO_UPDATE_CHECK=1 opts out.
func ChecksEnabled(updateChecks *bool) bool {
	if updateChecks != nil && !*updateChecks {
		return false
	}
	switch os.Getenv("OPCODE_NO_UPDATE_CHECK") {
	case "", "0", "false":
		return true
	}
	return false
}

// Pending returns cachedTag when it is strictly newer than current,
// and "" when there is nothing to offer: up to date, unknown, or a
// build that is not a release. Every surface — the startup note, the
// footer badge, `--version`, /doctor — asks here, so none of them can
// disagree about whether an update exists.
func Pending(current, cachedTag string) string {
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
		return cachedTag
	}
	return ""
}

// Notice is the startup-note sentence for a pending release: the same
// comparison as Pending, spelled out with the way to act on it.
func Notice(current, cachedTag string) string {
	tag := Pending(current, cachedTag)
	if tag == "" {
		return ""
	}
	return fmt.Sprintf("Update available: %s → %s. Run `opcode update` to install it.", current, tag)
}

// ReadCache returns the last attempt's outcome. A missing, oversized,
// or corrupt file reads as unchecked, never as an error the caller must
// surface.
func ReadCache(path string) (Cache, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Cache{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCacheBytes))
	if err != nil {
		return Cache{}, false
	}
	var c cacheFile
	if json.Unmarshal(data, &c) != nil {
		return Cache{}, false
	}
	t, err := time.Parse(time.RFC3339, c.CheckedAt)
	if err != nil {
		return Cache{}, false
	}
	return Cache{Tag: c.Tag, CheckedAt: t, Err: c.Err}, true
}

// WriteCache records one attempt, successful or not. Best-effort like
// the history file: a notice cache that cannot be written is a missed
// hint, not a failure.
func WriteCache(path string, c Cache) {
	if runes := []rune(c.Err); len(runes) > maxErrRunes {
		c.Err = string(runes[:maxErrRunes])
	}
	data, err := json.Marshal(cacheFile{
		Tag:       c.Tag,
		CheckedAt: c.CheckedAt.UTC().Format(time.RFC3339),
		Err:       c.Err,
	})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o600)
}

// Stale reports whether the cache needs another attempt: a working
// check lasts a day, a failed one an hour.
func Stale(c Cache, now time.Time) bool {
	ttl := cacheTTL
	if c.Err != "" {
		ttl = failureTTL
	}
	return now.Sub(c.CheckedAt) >= ttl
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

// Status is what one startup pass learned. Notice is the note to
// show; Tag is the newer release behind it, empty when there is none.
// The TUI renders Tag as a durable footer badge, and the exit line
// repeats it, so the note is never the only place it appeared.
type Status struct {
	Notice string
	Tag    string
}

// LatestKnown is the whole startup path in one call: read the cache,
// refresh it when stale (silent on failure), and report what is known.
// current is the running build version; home is the opcode home dir;
// apiURL is DefaultAPIURL in production. now is injectable for tests.
func LatestKnown(ctx context.Context, current, home, apiURL string, now time.Time) Status {
	// Dev builds never check: "newer" has no meaning when nothing
	// could be installed. (Platforms with no prebuilt release are
	// gated by the caller, which knows the running GOOS/GOARCH.)
	if !IsRelease(current) {
		return Status{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	path := CachePath(home)
	c, ok := ReadCache(path)
	if !ok || Stale(c, now) {
		fetchCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		defer cancel()
		fresh, err := FetchLatestTag(fetchCtx, apiURL, nil)
		switch {
		case err != nil:
			// Keep the last known tag — the release it names is still
			// real — but record the failure so the next launch waits
			// an hour instead of re-dialing, and /doctor can say so.
			c = Cache{Tag: c.Tag, CheckedAt: now, Err: err.Error()}
		case !IsRelease(fresh):
			// An upstream tag opcode cannot read is not a version.
			// Caching one would buy a day of silence under a tag no
			// surface can show, so it counts as a failed attempt.
			c = Cache{Tag: c.Tag, CheckedAt: now,
				Err: fmt.Sprintf("the latest release tag %q is not a vMAJOR.MINOR.PATCH version", fresh)}
		default:
			c = Cache{Tag: fresh, CheckedAt: now}
		}
		WriteCache(path, c)
	}
	tag := Pending(current, c.Tag)
	if tag == "" {
		return Status{}
	}
	return Status{Notice: Notice(current, c.Tag), Tag: tag}
}
