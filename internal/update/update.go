// Package update implements `tilde update`: fetch the latest GitHub
// release, verify its sha256 against the release's checksums.txt, and
// atomically replace the running binary.
//
// It only ever downloads on explicit user action (`tilde update`).
// Separately, the startup notice (cache.go) reads a cached latest-tag
// at launch and refreshes it at most once a day — the way a user on a
// stale binary learns an update exists. Standard library only.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultAPIURL resolves the latest non-prerelease, non-draft
	// release; its JSON carries the tag in "tag_name".
	DefaultAPIURL = "https://api.github.com/repos/Chmgx81/tilde/releases/latest"
	// DefaultDownloadBase is the release-asset prefix: <base>/<tag>/<asset>.
	DefaultDownloadBase = "https://github.com/Chmgx81/tilde/releases/download"

	// InstallOneLiner is the manual fallback when an in-place update is
	// not possible.
	InstallOneLiner = "curl -fsSL https://raw.githubusercontent.com/Chmgx81/tilde/main/install.sh | bash"
	releasesPage    = "https://github.com/Chmgx81/tilde/releases/latest"
	goInstall       = "go install github.com/Chmgx81/tilde/cmd/tilde@latest"

	maxAPIBody       = 1 << 20   // the release JSON is a few KB
	maxChecksumsBody = 1 << 20   // five lines
	maxArchiveBytes  = 100 << 20 // a compressed binary is ~10 MB
	maxBinaryBytes   = 200 << 20 // decompressed cap: a zip/gzip bomb guard

	defaultTimeout = 2 * time.Minute
)

var (
	// ErrDevBuild: the running binary is not a tagged release build, so
	// "newer" has no meaning and it is never replaced.
	ErrDevBuild = errors.New("not a release build")
	// ErrUnsupported: no prebuilt release exists for this platform.
	ErrUnsupported = errors.New("unsupported platform")
	// ErrChecksum: the download does not match its published sha256.
	ErrChecksum = errors.New("checksum mismatch")
	// ErrNoChecksum: checksums.txt has no usable entry for the asset.
	ErrNoChecksum = errors.New("no checksum entry")
	// ErrArchive: the archive is corrupt, oversized, or unsafe.
	ErrArchive = errors.New("bad archive")
	// ErrReplace: the verified binary could not be put in place; the
	// old binary is untouched.
	ErrReplace = errors.New("cannot replace the running binary")
)

// prebuilt is the release matrix (.github/workflows/release.yml).
var prebuilt = map[string]bool{
	"linux/amd64":   true,
	"linux/arm64":   true,
	"darwin/amd64":  true,
	"darwin/arm64":  true,
	"windows/amd64": true,
}

// Options configures Run. Only Current, CheckOnly and Out are needed in
// production; the rest exist so tests never touch the real network or
// the real executable, and zero values mean production defaults.
type Options struct {
	Current   string    // running version, e.g. "v0.2.0" or "(devel)"
	CheckOnly bool      // report only; download nothing, change nothing
	Out       io.Writer // human-readable progress; nil discards
	// CacheHome, when non-empty, is the tilde home dir whose
	// .update-cache.json records the latest tag this run saw — so
	// the startup notice stays fresh after an explicit check.
	CacheHome string

	APIURL        string
	DownloadBase  string
	GOOS, GOARCH  string
	ExecPath      string // binary to replace; default: this executable
	Client        *http.Client
	MaxBinarySize int64 // cap on the extracted binary; default 200 MiB
}

func (o Options) withDefaults() Options {
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.APIURL == "" {
		o.APIURL = DefaultAPIURL
	}
	if o.DownloadBase == "" {
		o.DownloadBase = DefaultDownloadBase
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: defaultTimeout, CheckRedirect: refuseHTTPSDowngrade}
	}
	if o.MaxBinarySize == 0 {
		o.MaxBinarySize = maxBinaryBytes
	}
	return o
}

// refuseHTTPSDowngrade stops a redirect from an https URL to plain
// http, which would let a network attacker read or swap the download.
func refuseHTTPSDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if req.URL.Scheme != "https" && via[len(via)-1].URL.Scheme == "https" {
		return fmt.Errorf("refusing redirect from https to %s", req.URL.Scheme)
	}
	return nil
}

// Run checks for a newer release and, unless CheckOnly, installs it.
// It returns nil when already up to date. Every failure path leaves
// the installed binary untouched.
func Run(ctx context.Context, o Options) error {
	o = o.withDefaults()

	cur, ok := parseRelease(o.Current)
	if !ok {
		return fmt.Errorf("%w: this build reports version %q, so tilde cannot tell whether it is out of date — install a release with: %s",
			ErrDevBuild, o.Current, InstallOneLiner)
	}
	platform := o.GOOS + "/" + o.GOARCH
	if !prebuilt[platform] {
		return fmt.Errorf("%w: no prebuilt tilde for %s — install with: %s", ErrUnsupported, platform, goInstall)
	}

	tag, err := latestTag(ctx, o)
	if err != nil {
		return err
	}
	latest, ok := parseRelease(tag)
	if !ok {
		return fmt.Errorf("the latest release tag %q is not a vMAJOR.MINOR.PATCH version; refusing to use it", tag)
	}
	// The explicit check is the freshest signal the startup notice
	// has: record it for the next launch, replacing any recorded
	// failure. After the tag is validated, so a malformed response
	// never lands in the cache as a version. Best-effort — a cache
	// write failure must not fail the update itself.
	if o.CacheHome != "" {
		WriteCache(CachePath(o.CacheHome), Cache{Tag: tag, CheckedAt: time.Now()})
	}

	switch c := compare(cur, latest); {
	case c == 0:
		fmt.Fprintf(o.Out, "tilde %s is up to date.\n", o.Current)
		return nil
	case c > 0:
		fmt.Fprintf(o.Out, "tilde %s is newer than the latest release (%s); not downgrading.\n", o.Current, tag)
		return nil
	}

	if o.CheckOnly {
		fmt.Fprintf(o.Out, "Update available: %s -> %s. Run `tilde update` to install it.\n", o.Current, tag)
		return nil
	}

	target := o.ExecPath
	if target == "" {
		if target, err = currentExecutable(); err != nil {
			return fmt.Errorf("locate the running binary: %w", err)
		}
	}

	asset := assetName(o.GOOS, o.GOARCH)
	base := strings.TrimRight(o.DownloadBase, "/") + "/" + tag + "/"
	fmt.Fprintf(o.Out, "Updating tilde %s -> %s (%s)...\n", o.Current, tag, platform)

	archive, err := o.get(ctx, base+asset, maxArchiveBytes, "")
	if err != nil {
		return err
	}
	sums, err := o.get(ctx, base+"checksums.txt", maxChecksumsBody, "")
	if err != nil {
		return err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return err
	}
	if got := sha256Hex(archive); got != want {
		return fmt.Errorf("%w for %s: expected %s, got %s — the download is corrupted or tampered with; nothing was installed",
			ErrChecksum, asset, want, got)
	}

	if err := install(target, archive, o); err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "✓ tilde updated to %s (%s)\n", tag, target)
	return nil
}

func assetName(goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return "tilde-" + goos + "-" + goarch + ext
}

// binaryNames are the archive entries accepted as the binary: what the
// release workflow packs, and the plain name the installer also allows.
func binaryNames(goos, goarch string) []string {
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	return []string{"tilde-" + goos + "-" + goarch + ext, "tilde" + ext}
}

func currentExecutable() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

func latestTag(ctx context.Context, o Options) (string, error) {
	body, err := o.get(ctx, o.APIURL, maxAPIBody, "application/vnd.github+json")
	if err != nil {
		return "", err
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", fmt.Errorf("parse latest-release response: %w", err)
	}
	if rel.TagName == "" {
		return "", errors.New("latest-release response has no tag_name")
	}
	return rel.TagName, nil
}

// get fetches url, requiring HTTP 200 and at most limit bytes.
func (o Options) get(ctx context.Context, url string, limit int64, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "tilde-update")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		hint := ""
		switch resp.StatusCode {
		case http.StatusForbidden, http.StatusTooManyRequests:
			hint = " (rate limited? try again later)"
		case http.StatusNotFound:
			hint = " (not found — is there a published release?)"
		}
		return nil, fmt.Errorf("GET %s: HTTP %d%s", url, resp.StatusCode, hint)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("GET %s: response larger than the %d-byte limit", url, limit)
	}
	return body, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// checksumFor finds asset in sha256sum-format text ("<hex>  <name>" or
// "<hex> *<name>"). The name is compared exactly, never as a pattern.
// Two entries that disagree are an error rather than a guess.
func checksumFor(sums []byte, asset string) (string, error) {
	found := ""
	for _, line := range strings.Split(string(sums), "\n") {
		digest, rest, ok := strings.Cut(strings.TrimRight(line, "\r"), " ")
		if !ok {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(rest, " "), "*")
		if name != asset {
			continue
		}
		raw, err := hex.DecodeString(digest)
		if err != nil || len(raw) != sha256.Size {
			return "", fmt.Errorf("%w: malformed digest for %s in checksums.txt", ErrNoChecksum, asset)
		}
		digest = strings.ToLower(digest)
		if found != "" && found != digest {
			return "", fmt.Errorf("%w: conflicting entries for %s in checksums.txt", ErrNoChecksum, asset)
		}
		found = digest
	}
	if found == "" {
		return "", fmt.Errorf("%w for %s in checksums.txt; nothing was installed", ErrNoChecksum, asset)
	}
	return found, nil
}

// install writes the verified binary next to target and renames it
// into place. The temp file lives in target's directory so the rename
// is atomic (same filesystem); on any failure it is removed and the
// old binary is untouched.
func install(target string, archive []byte, o Options) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".tilde-update-*")
	if err != nil {
		return replaceError(target, err, o.GOOS)
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(name)
		}
	}()

	if err := extractBinary(archive, o.GOOS, o.GOARCH, o.MaxBinarySize, tmp); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return replaceError(target, err, o.GOOS)
	}
	if err := os.Chmod(name, 0o755); err != nil {
		return replaceError(target, err, o.GOOS)
	}
	if err := os.Rename(name, target); err != nil {
		return replaceError(target, err, o.GOOS)
	}
	return nil
}

func replaceError(target string, err error, goos string) error {
	how := "re-run the installer: " + InstallOneLiner
	if goos == "windows" {
		how = "download the .zip from " + releasesPage + " and replace the .exe by hand"
	}
	return fmt.Errorf("%w: %v\n%s was left unchanged. To update manually, %s (if it is in a protected directory, use sudo or set TILDE_INSTALL_DIR)",
		ErrReplace, err, target, how)
}

// extractBinary copies the one expected binary out of the archive into
// w, without ever using an entry's name as a filesystem path. Entries
// with unsafe names, links, devices, or more than limit bytes fail the
// whole update.
func extractBinary(archive []byte, goos, goarch string, limit int64, w io.Writer) error {
	wanted := binaryNames(goos, goarch)
	if goos == "windows" {
		return extractZip(archive, wanted, limit, w)
	}
	return extractTarGz(archive, wanted, limit, w)
}

func isWanted(name string, wanted []string) bool {
	for _, w := range wanted {
		if name == w {
			return true
		}
	}
	return false
}

// entryName returns the cleaned entry name, or false when it is
// absolute, has a ".." element, a backslash, or a NUL.
func entryName(name string) (string, bool) {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") {
		return "", false
	}
	name = strings.TrimPrefix(name, "./")
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", false
		}
	}
	return name, true
}

func archiveErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrArchive, fmt.Sprintf(format, args...))
}

func extractTarGz(archive []byte, wanted []string, limit int64, w io.Writer) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return archiveErr("not a gzip stream: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	copied := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return archiveErr("read tar: %v", err)
		}
		name, ok := entryName(hdr.Name)
		if !ok {
			return archiveErr("unsafe entry name %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir, tar.TypeXGlobalHeader:
			continue
		case tar.TypeReg:
		default:
			return archiveErr("entry %q is not a regular file (type %q)", hdr.Name, hdr.Typeflag)
		}
		if !isWanted(name, wanted) {
			continue
		}
		if copied {
			return archiveErr("more than one %q entry", name)
		}
		if hdr.Size > limit {
			return archiveErr("entry %q is %d bytes, over the %d-byte limit", name, hdr.Size, limit)
		}
		if err := copyCapped(w, tr, name, limit); err != nil {
			return err
		}
		copied = true
	}
	if !copied {
		return archiveErr("no %s entry", wanted[0])
	}
	// The tar trailer ends before the gzip trailer; drain (bounded) so a
	// bad gzip CRC or truncation is reported instead of ignored.
	if _, err := io.Copy(io.Discard, io.LimitReader(gz, limit)); err != nil {
		return archiveErr("read gzip trailer: %v", err)
	}
	return nil
}

func extractZip(archive []byte, wanted []string, limit int64, w io.Writer) error {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return archiveErr("not a zip file: %v", err)
	}
	copied := false
	for _, f := range zr.File {
		name, ok := entryName(f.Name)
		if !ok {
			return archiveErr("unsafe entry name %q", f.Name)
		}
		mode := f.Mode()
		if mode.IsDir() {
			continue
		}
		if !mode.IsRegular() {
			return archiveErr("entry %q is not a regular file", f.Name)
		}
		if !isWanted(name, wanted) {
			continue
		}
		if copied {
			return archiveErr("more than one %q entry", name)
		}
		if f.UncompressedSize64 > uint64(limit) {
			return archiveErr("entry %q is %d bytes, over the %d-byte limit", name, f.UncompressedSize64, limit)
		}
		rc, err := f.Open()
		if err != nil {
			return archiveErr("open %q: %v", f.Name, err)
		}
		err = copyCapped(w, rc, name, limit)
		rc.Close()
		if err != nil {
			return err
		}
		copied = true
	}
	if !copied {
		return archiveErr("no %s entry", wanted[0])
	}
	return nil
}

// copyCapped copies at most limit bytes: the header's declared size is
// not trusted, so the stream itself is bounded too.
func copyCapped(w io.Writer, r io.Reader, name string, limit int64) error {
	n, err := io.Copy(w, io.LimitReader(r, limit+1))
	if err != nil {
		return archiveErr("read %q: %v", name, err)
	}
	if n > limit {
		return archiveErr("entry %q is over the %d-byte limit", name, limit)
	}
	if n == 0 {
		return archiveErr("entry %q is empty", name)
	}
	return nil
}
