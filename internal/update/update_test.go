package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	oldBinary = "old-binary"
	newBinary = "#!/bin/sh\necho new\n"
	curTag    = "v1.0.0"
	newTag    = "v1.1.0"
	linuxAsst = "tilde-linux-amd64.tar.gz"
	linuxBin  = "tilde-linux-amd64"
)

type tarEntry struct {
	name string
	typ  byte // 0 means a regular file
	body string
	link string
}

func tarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o755, Linkname: e.link}
		if typ == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sumLine(data []byte, name string) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:]) + "  " + name + "\n"
}

// fakeRelease serves the two endpoints Run uses. downloads counts every
// request other than the latest-release lookup.
type fakeRelease struct {
	*httptest.Server
	downloads atomic.Int32
}

func serve(t *testing.T, tag string, files map[string][]byte) *fakeRelease {
	t.Helper()
	fr := &fakeRelease{}
	fr.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			fmt.Fprintf(w, `{"tag_name": %q, "name": "ignored"}`, tag)
			return
		}
		fr.downloads.Add(1)
		prefix := "/dl/" + tag + "/"
		if name, ok := strings.CutPrefix(r.URL.Path, prefix); ok {
			if data, ok := files[name]; ok {
				w.Write(data)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(fr.Close)
	return fr
}

func linuxRelease(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		linuxAsst:       archive,
		"checksums.txt": []byte(sumLine(archive, linuxAsst)),
	}
}

func goodArchive(t *testing.T) []byte {
	return tarGz(t, tarEntry{name: linuxBin, body: newBinary})
}

// target creates an "installed" binary in a fresh dir.
func target(t *testing.T) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "tilde")
	if err := os.WriteFile(path, []byte(oldBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func opts(srv *httptest.Server, path string, out *bytes.Buffer) Options {
	o := Options{
		Current:      curTag,
		APIURL:       srv.URL + "/latest",
		DownloadBase: srv.URL + "/dl",
		GOOS:         "linux",
		GOARCH:       "amd64",
		ExecPath:     path,
		Client:       &http.Client{Timeout: 5 * time.Second},
	}
	if out != nil { // a typed-nil *Buffer in the io.Writer would panic
		o.Out = out
	}
	return o
}

func assertUntouched(t *testing.T, dir, path string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != oldBinary {
		t.Fatalf("installed binary changed: %q, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("stray files left in the install dir: %v", names)
	}
}

func TestUpdateReplacesBinary(t *testing.T) {
	srv := serve(t, newTag, linuxRelease(t, goodArchive(t)))
	dir, path := target(t)
	var out bytes.Buffer

	if err := Run(context.Background(), opts(srv.Server, path, &out)); err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != newBinary {
		t.Fatalf("binary = %q, %v; want the new one", got, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o755 {
			t.Errorf("mode = %v, want 0755", fi.Mode().Perm())
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
	if !strings.Contains(out.String(), newTag) {
		t.Errorf("output should name the new version: %q", out.String())
	}
}

func TestUpdateAcceptsPlainBinaryName(t *testing.T) {
	archive := tarGz(t, tarEntry{name: "./tilde", body: newBinary})
	srv := serve(t, newTag, linuxRelease(t, archive))
	_, path := target(t)
	if err := Run(context.Background(), opts(srv.Server, path, nil)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != newBinary {
		t.Fatalf("binary = %q", got)
	}
}

func TestUpdateWindowsZip(t *testing.T) {
	const asset = "tilde-windows-amd64.zip"
	archive := zipOf(t, map[string]string{"tilde-windows-amd64.exe": newBinary})
	srv := serve(t, newTag, map[string][]byte{
		asset:           archive,
		"checksums.txt": []byte(sumLine(archive, asset)),
	})
	_, path := target(t)
	o := opts(srv.Server, path, nil)
	o.GOOS = "windows"
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != newBinary {
		t.Fatalf("binary = %q", got)
	}
}

func TestUpToDate(t *testing.T) {
	srv := serve(t, curTag, nil)
	dir, path := target(t)
	var out bytes.Buffer
	if err := Run(context.Background(), opts(srv.Server, path, &out)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("output = %q", out.String())
	}
	if n := srv.downloads.Load(); n != 0 {
		t.Errorf("%d downloads for an up-to-date binary", n)
	}
	assertUntouched(t, dir, path)
}

func TestNeverDowngrades(t *testing.T) {
	for _, current := range []string{"v2.0.0", "v1.1.1", "v1.2.0-rc.1"} {
		t.Run(current, func(t *testing.T) {
			srv := serve(t, newTag, nil) // latest is v1.1.0
			dir, path := target(t)
			var out bytes.Buffer
			o := opts(srv.Server, path, &out)
			o.Current = current
			if err := Run(context.Background(), o); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "not downgrading") {
				t.Errorf("output = %q", out.String())
			}
			if srv.downloads.Load() != 0 {
				t.Error("downloaded despite being newer than latest")
			}
			assertUntouched(t, dir, path)
		})
	}
}

func TestPrereleaseUpgradesToItsRelease(t *testing.T) {
	srv := serve(t, "v1.0.0", linuxRelease(t, goodArchive(t)))
	_, path := target(t)
	o := opts(srv.Server, path, nil)
	o.Current = "v1.0.0-rc.1"
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != newBinary {
		t.Fatalf("rc build was not upgraded to its release")
	}
}

func TestCheckOnly(t *testing.T) {
	srv := serve(t, newTag, linuxRelease(t, goodArchive(t)))
	dir, path := target(t)
	var out bytes.Buffer
	o := opts(srv.Server, path, &out)
	o.CheckOnly = true
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Update available: v1.0.0 -> v1.1.0") {
		t.Errorf("output = %q", out.String())
	}
	if srv.downloads.Load() != 0 {
		t.Error("--check downloaded something")
	}
	assertUntouched(t, dir, path)
}

// TestRunRefreshesTheNoticeCache: an explicit check is the freshest
// signal the startup notice has, so it records the tag — and clears a
// failure an earlier launch left behind.
func TestRunRefreshesTheNoticeCache(t *testing.T) {
	srv := serve(t, newTag, linuxRelease(t, goodArchive(t)))
	dir, path := target(t)
	home := t.TempDir()
	WriteCache(CachePath(home), Cache{Tag: "v0.9.0", CheckedAt: time.Now().Add(-72 * time.Hour), Err: "offline"})

	o := opts(srv.Server, path, nil)
	o.CheckOnly = true
	o.CacheHome = home
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	c, ok := ReadCache(CachePath(home))
	if !ok || c.Tag != newTag || c.Err != "" {
		t.Errorf("cache = %+v %v, want a clean %s", c, ok, newTag)
	}
	if got := Pending(curTag, c.Tag); got != newTag {
		t.Errorf("Pending after the check = %q, want %q — the next launch must show it", got, newTag)
	}
	assertUntouched(t, dir, path)
}

// TestRunCachesNothingWhenTheTagIsJunk: a malformed latest response
// must not land in the cache as a version, or the notice would go
// quiet for a day over a transient upstream glitch.
func TestRunCachesNothingWhenTheTagIsJunk(t *testing.T) {
	srv := serve(t, "nightly", linuxRelease(t, goodArchive(t)))
	_, path := target(t)
	home := t.TempDir()
	o := opts(srv.Server, path, nil)
	o.CheckOnly = true
	o.CacheHome = home
	if err := Run(context.Background(), o); err == nil {
		t.Fatal("a junk tag was accepted")
	}
	if _, ok := ReadCache(CachePath(home)); ok {
		t.Error("a rejected tag must not be cached")
	}
}

func TestDevBuildsAreNotUpdated(t *testing.T) {
	for _, current := range []string{
		"(devel)", "dev", "", "v1.0.0+dirty", "v1.0.1-0.20250101000000-abcdef123456",
	} {
		t.Run(current, func(t *testing.T) {
			srv := serve(t, newTag, linuxRelease(t, goodArchive(t)))
			dir, path := target(t)
			o := opts(srv.Server, path, nil)
			o.Current = current
			err := Run(context.Background(), o)
			if !errors.Is(err, ErrDevBuild) {
				t.Fatalf("err = %v, want ErrDevBuild", err)
			}
			if !strings.Contains(err.Error(), InstallOneLiner) {
				t.Errorf("error should point at the installer: %v", err)
			}
			if srv.downloads.Load() != 0 {
				t.Error("dev build touched the release assets")
			}
			assertUntouched(t, dir, path)
		})
	}
}

func TestUnsupportedPlatform(t *testing.T) {
	for _, p := range [][2]string{{"plan9", "amd64"}, {"linux", "riscv64"}, {"windows", "arm64"}} {
		t.Run(p[0]+"/"+p[1], func(t *testing.T) {
			srv := serve(t, newTag, linuxRelease(t, goodArchive(t)))
			dir, path := target(t)
			o := opts(srv.Server, path, nil)
			o.GOOS, o.GOARCH = p[0], p[1]
			err := Run(context.Background(), o)
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported", err)
			}
			if !strings.Contains(err.Error(), "go install") {
				t.Errorf("error should offer go install: %v", err)
			}
			assertUntouched(t, dir, path)
		})
	}
}

func TestChecksumMismatchLeavesBinaryUntouched(t *testing.T) {
	archive := goodArchive(t)
	files := linuxRelease(t, archive)
	files["checksums.txt"] = []byte(sumLine([]byte("something else"), linuxAsst))
	srv := serve(t, newTag, files)
	dir, path := target(t)

	err := Run(context.Background(), opts(srv.Server, path, nil))
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("err = %v, want ErrChecksum", err)
	}
	assertUntouched(t, dir, path)
}

func TestTamperedArchiveIsRefused(t *testing.T) {
	archive := goodArchive(t)
	files := linuxRelease(t, archive) // checksums describe the honest archive
	files[linuxAsst] = tarGz(t, tarEntry{name: linuxBin, body: "malware"})
	srv := serve(t, newTag, files)
	dir, path := target(t)

	if err := Run(context.Background(), opts(srv.Server, path, nil)); !errors.Is(err, ErrChecksum) {
		t.Fatalf("err = %v, want ErrChecksum", err)
	}
	assertUntouched(t, dir, path)
}

func TestMissingChecksums(t *testing.T) {
	archive := goodArchive(t)
	cases := map[string]struct {
		sums    *string // nil: checksums.txt is absent (404)
		wantErr error
		wantMsg string
	}{
		"file absent":   {sums: nil, wantMsg: "HTTP 404"},
		"entry missing": {sums: ptr(sumLine(archive, "tilde-linux-arm64.tar.gz")), wantErr: ErrNoChecksum},
		"empty file":    {sums: ptr(""), wantErr: ErrNoChecksum},
		"bad digest":    {sums: ptr("nothex  " + linuxAsst + "\n"), wantErr: ErrNoChecksum},
		"short digest":  {sums: ptr("abcd  " + linuxAsst + "\n"), wantErr: ErrNoChecksum},
		"conflicting": {
			sums:    ptr(sumLine(archive, linuxAsst) + sumLine([]byte("x"), linuxAsst)),
			wantErr: ErrNoChecksum,
		},
		// A prefix or suffix match must not count as the entry.
		"only a longer name": {sums: ptr(sumLine(archive, linuxAsst+".sig")), wantErr: ErrNoChecksum},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			files := map[string][]byte{linuxAsst: archive}
			if c.sums != nil {
				files["checksums.txt"] = []byte(*c.sums)
			}
			srv := serve(t, newTag, files)
			dir, path := target(t)
			err := Run(context.Background(), opts(srv.Server, path, nil))
			if err == nil {
				t.Fatal("update succeeded without a usable checksum")
			}
			if c.wantErr != nil && !errors.Is(err, c.wantErr) {
				t.Errorf("err = %v, want %v", err, c.wantErr)
			}
			if c.wantMsg != "" && !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("err = %v, want it to contain %q", err, c.wantMsg)
			}
			assertUntouched(t, dir, path)
		})
	}
}

func ptr(s string) *string { return &s }

func TestChecksumFormats(t *testing.T) {
	data := []byte("payload")
	s := sha256.Sum256(data)
	digest := hex.EncodeToString(s[:])
	for name, text := range map[string]string{
		"text mode":    digest + "  " + linuxAsst + "\n",
		"binary mode":  digest + " *" + linuxAsst + "\n",
		"crlf":         digest + "  " + linuxAsst + "\r\n",
		"upper hex":    strings.ToUpper(digest) + "  " + linuxAsst + "\n",
		"among others": "0000  other\n" + digest + "  " + linuxAsst + "\n" + digest + "  " + linuxAsst + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := checksumFor([]byte(text), linuxAsst)
			if err != nil || got != digest {
				t.Fatalf("checksumFor = %q, %v; want %q", got, err, digest)
			}
		})
	}
}

func TestBadArchivesAreRefused(t *testing.T) {
	cases := map[string][]byte{
		"not gzip":         []byte("this is not a tarball"),
		"empty tar":        tarGz(t),
		"binary missing":   tarGz(t, tarEntry{name: "README", body: "hi"}),
		"empty binary":     tarGz(t, tarEntry{name: linuxBin, body: ""}),
		"duplicate binary": tarGz(t, tarEntry{name: linuxBin, body: "a"}, tarEntry{name: "./" + linuxBin, body: "b"}),
		"traversal":        tarGz(t, tarEntry{name: "../" + linuxBin, body: newBinary}),
		"traversal nested": tarGz(t, tarEntry{name: "a/../../evil", body: "x"}, tarEntry{name: linuxBin, body: newBinary}),
		"absolute":         tarGz(t, tarEntry{name: "/etc/passwd", body: "x"}),
		"backslash":        tarGz(t, tarEntry{name: `..\evil`, body: "x"}),
		"symlink":          tarGz(t, tarEntry{name: linuxBin, typ: tar.TypeSymlink, link: "/etc/passwd"}),
		"hardlink":         tarGz(t, tarEntry{name: linuxBin, typ: tar.TypeLink, link: "/etc/passwd"}),
		"decoy symlink":    tarGz(t, tarEntry{name: "x", typ: tar.TypeSymlink, link: "/etc"}, tarEntry{name: linuxBin, body: newBinary}),
		"device":           tarGz(t, tarEntry{name: linuxBin, typ: tar.TypeChar}),
		"binary in subdir": tarGz(t, tarEntry{name: "dist/" + linuxBin, body: newBinary}),
	}
	whole := goodArchive(t)
	cases["truncated gzip"] = whole[:len(whole)/2]
	corrupt := append([]byte(nil), whole...)
	corrupt[len(corrupt)-6] ^= 0xff // inside the gzip CRC/size trailer
	cases["corrupt trailer"] = corrupt

	for name, archive := range cases {
		t.Run(name, func(t *testing.T) {
			// The checksum matches the (bad) archive: this exercises
			// extraction, not verification.
			srv := serve(t, newTag, linuxRelease(t, archive))
			dir, path := target(t)
			err := Run(context.Background(), opts(srv.Server, path, nil))
			if !errors.Is(err, ErrArchive) {
				t.Fatalf("err = %v, want ErrArchive", err)
			}
			assertUntouched(t, dir, path)
		})
	}
}

func TestOversizedBinaryIsRefused(t *testing.T) {
	archive := tarGz(t, tarEntry{name: linuxBin, body: strings.Repeat("A", 4096)})
	srv := serve(t, newTag, linuxRelease(t, archive))
	dir, path := target(t)
	o := opts(srv.Server, path, nil)
	o.MaxBinarySize = 1024
	err := Run(context.Background(), o)
	if !errors.Is(err, ErrArchive) || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("err = %v, want ErrArchive mentioning the limit", err)
	}
	assertUntouched(t, dir, path)
}

func TestOversizedZipEntryIsRefused(t *testing.T) {
	const asset = "tilde-windows-amd64.zip"
	archive := zipOf(t, map[string]string{"tilde-windows-amd64.exe": strings.Repeat("A", 4096)})
	srv := serve(t, newTag, map[string][]byte{
		asset:           archive,
		"checksums.txt": []byte(sumLine(archive, asset)),
	})
	dir, path := target(t)
	o := opts(srv.Server, path, nil)
	o.GOOS = "windows"
	o.MaxBinarySize = 1024
	if err := Run(context.Background(), o); !errors.Is(err, ErrArchive) {
		t.Fatalf("err = %v, want ErrArchive", err)
	}
	assertUntouched(t, dir, path)
}

func TestBadZipIsRefused(t *testing.T) {
	const asset = "tilde-windows-amd64.zip"
	for name, archive := range map[string][]byte{
		"not a zip":  []byte("nope"),
		"no binary":  zipOf(t, map[string]string{"README": "hi"}),
		"traversal":  zipOf(t, map[string]string{"../tilde-windows-amd64.exe": "x"}),
		"other name": zipOf(t, map[string]string{"tilde-linux-amd64": "x"}),
	} {
		t.Run(name, func(t *testing.T) {
			srv := serve(t, newTag, map[string][]byte{
				asset:           archive,
				"checksums.txt": []byte(sumLine(archive, asset)),
			})
			dir, path := target(t)
			o := opts(srv.Server, path, nil)
			o.GOOS = "windows"
			if err := Run(context.Background(), o); !errors.Is(err, ErrArchive) {
				t.Fatalf("err = %v, want ErrArchive", err)
			}
			assertUntouched(t, dir, path)
		})
	}
}

func TestHTTPFailures(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			// Latest-release lookup fails.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", status)
			}))
			defer srv.Close()
			dir, path := target(t)
			err := Run(context.Background(), opts(srv, path, nil))
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) {
				t.Fatalf("err = %v, want HTTP %d", err, status)
			}
			assertUntouched(t, dir, path)
		})
	}
}

func TestArchiveDownloadFailure(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/latest" {
					fmt.Fprintf(w, `{"tag_name": %q}`, newTag)
					return
				}
				http.Error(w, "boom", status)
			}))
			defer srv.Close()
			dir, path := target(t)
			err := Run(context.Background(), opts(srv, path, nil))
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) {
				t.Fatalf("err = %v, want HTTP %d", err, status)
			}
			assertUntouched(t, dir, path)
		})
	}
}

func TestMalformedLatestResponses(t *testing.T) {
	for name, body := range map[string]string{
		"not json":       "<html>",
		"no tag":         `{"name": "x"}`,
		"empty tag":      `{"tag_name": ""}`,
		"not semver":     `{"tag_name": "latest"}`,
		"path injection": `{"tag_name": "v9.9.9/../../evil"}`,
		"build metadata": `{"tag_name": "v9.9.9+x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var downloads atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/latest" {
					downloads.Add(1)
				}
				fmt.Fprint(w, body)
			}))
			defer srv.Close()
			dir, path := target(t)
			if err := Run(context.Background(), opts(srv, path, nil)); err == nil {
				t.Fatal("Run accepted a bad latest-release response")
			}
			if downloads.Load() != 0 {
				t.Error("fetched assets after a bad tag")
			}
			assertUntouched(t, dir, path)
		})
	}
}

func TestOversizedAPIResponseIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name": "v9.0.0", "pad": %q}`, strings.Repeat("x", maxAPIBody))
	}))
	defer srv.Close()
	_, path := target(t)
	err := Run(context.Background(), opts(srv, path, nil))
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("err = %v, want a size-limit error", err)
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	dir, path := target(t)
	o := opts(srv, path, nil)
	o.Client = &http.Client{Timeout: 100 * time.Millisecond}

	start := time.Now()
	err := Run(context.Background(), o)
	if err == nil {
		t.Fatal("Run succeeded against a hung server")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("timeout took %v", elapsed)
	}
	assertUntouched(t, dir, path)
}

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	_, path := target(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := Run(ctx, opts(srv, path, nil)); err == nil {
		t.Fatal("Run ignored context cancellation")
	}
}

func TestPermissionDeniedFallback(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not bind on windows or as root")
	}
	srv := serve(t, newTag, linuxRelease(t, goodArchive(t)))
	dir, path := target(t)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	err := Run(context.Background(), opts(srv.Server, path, nil))
	if !errors.Is(err, ErrReplace) {
		t.Fatalf("err = %v, want ErrReplace", err)
	}
	for _, want := range []string{InstallOneLiner, "unchanged", path} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q:\n%v", want, err)
		}
	}
	os.Chmod(dir, 0o755)
	assertUntouched(t, dir, path)
}

func TestRenameFailureFallback(t *testing.T) {
	srv := serve(t, newTag, linuxRelease(t, goodArchive(t)))
	dir := t.TempDir()
	// A non-empty directory cannot be replaced by a file, so the rename
	// fails after the temp file was written and must be cleaned up.
	blocker := filepath.Join(dir, "tilde")
	if err := os.MkdirAll(filepath.Join(blocker, "child"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := Run(context.Background(), opts(srv.Server, blocker, nil))
	if !errors.Is(err, ErrReplace) || !strings.Contains(err.Error(), InstallOneLiner) {
		t.Fatalf("err = %v, want ErrReplace with the installer fallback", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "tilde" {
		t.Errorf("temp file not cleaned up: %v", entries)
	}
}

func TestWindowsFallbackDoesNotSuggestBash(t *testing.T) {
	err := replaceError(`C:\tilde.exe`, errors.New("access denied"), "windows")
	if strings.Contains(err.Error(), "curl") || !strings.Contains(err.Error(), "releases") {
		t.Errorf("windows fallback = %v", err)
	}
}

func TestRefuseHTTPSDowngrade(t *testing.T) {
	mk := func(raw string) *http.Request {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Request{URL: u}
	}
	if err := refuseHTTPSDowngrade(mk("http://evil.example/x"), []*http.Request{mk("https://github.com/x")}); err == nil {
		t.Error("https -> http redirect was allowed")
	}
	if err := refuseHTTPSDowngrade(mk("https://objects.example/x"), []*http.Request{mk("https://github.com/x")}); err != nil {
		t.Errorf("https -> https redirect refused: %v", err)
	}
	if err := refuseHTTPSDowngrade(mk("http://127.0.0.1/x"), []*http.Request{mk("http://127.0.0.1/y")}); err != nil {
		t.Errorf("http -> http (tests only) refused: %v", err)
	}
	via := make([]*http.Request, 10)
	for i := range via {
		via[i] = mk("https://github.com/x")
	}
	if err := refuseHTTPSDowngrade(mk("https://github.com/x"), via); err == nil {
		t.Error("redirect loop was not stopped")
	}
}

func TestDefaultsPointAtTheRealRepoOverHTTPS(t *testing.T) {
	for _, u := range []string{DefaultAPIURL, DefaultDownloadBase, InstallOneLiner} {
		if !strings.Contains(u, "https://") || !strings.Contains(u, "Chmgx81/tilde") {
			t.Errorf("unexpected default %q", u)
		}
	}
}
