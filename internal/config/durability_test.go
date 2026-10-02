package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestAuthWritersEnforcePrivateModeOnExistingFile(t *testing.T) {
	// A pre-existing auth.json with loose permissions must not stay
	// world-readable after tilde rewrites it: os.WriteFile only applies
	// its mode when it creates the file.
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	writers := map[string]func(dir string) error{
		"WriteAuthKey": func(dir string) error { return WriteAuthKey(dir, "b", "new-key") },
		"RemoveAuthKey": func(dir string) error {
			_, err := RemoveAuthKey(dir, "a")
			return err
		},
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "auth.json")
			if err := os.WriteFile(path, []byte(`{"a":"old-key"}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o644); err != nil { // defeat umask
				t.Fatal(err)
			}
			if err := write(dir); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); perm != 0o600 {
				t.Errorf("auth.json mode after %s = %v, want 0600", name, perm)
			}
		})
	}
}

func TestConcurrentWriteAuthKeyKeepsEveryProvider(t *testing.T) {
	// /login from two places at once must not lose a credential to a
	// read-modify-write race.
	dir := t.TempDir()
	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- WriteAuthKey(dir, fmt.Sprintf("p%02d", i), fmt.Sprintf("key-%02d", i))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("WriteAuthKey: %v", err)
		}
	}
	auth, _, err := LoadAuth(dir)
	if err != nil {
		t.Fatalf("LoadAuth after concurrent writes: %v", err)
	}
	for i := 0; i < n; i++ {
		p, want := fmt.Sprintf("p%02d", i), fmt.Sprintf("key-%02d", i)
		if auth[p] != want {
			t.Errorf("provider %s lost or corrupted: got %q, want %q", p, auth[p], want)
		}
	}
}

func TestAuthReadersNeverSeeATornFile(t *testing.T) {
	// A reader racing a writer must see the old file or the new one,
	// never a truncated prefix. Truncate-then-write exposes an empty
	// or partial file; write-temp-then-rename does not.
	dir := t.TempDir()
	if err := WriteAuthKey(dir, "seed", "seed-key"); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("k", 1<<20)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var torn error
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, _, err := LoadAuth(dir); err != nil {
				mu.Lock()
				torn = err
				mu.Unlock()
				return
			}
		}
	}()
	for i := 0; i < 60; i++ {
		if err := WriteAuthKey(dir, "big", big+fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if torn != nil {
		t.Fatalf("reader saw a torn auth.json: %v", torn)
	}
}

func TestWriteAuthKeyRefusesToOverwriteCorruptFile(t *testing.T) {
	// A malformed auth.json holds credentials tilde cannot parse.
	// Overwriting it would destroy them; the write must fail instead.
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	corrupt := []byte(`{"openrouter": "sk-precious",`)
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAuthKey(dir, "other", "k"); err == nil {
		t.Fatal("WriteAuthKey over a corrupt file must fail")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(corrupt) {
		t.Errorf("corrupt file was modified: %q", got)
	}
	if _, err := RemoveAuthKey(dir, "openrouter"); err == nil {
		t.Error("RemoveAuthKey over a corrupt file must fail")
	}
	got, _ = os.ReadFile(path)
	if string(got) != string(corrupt) {
		t.Errorf("corrupt file was modified by RemoveAuthKey: %q", got)
	}
}

func TestAuthWritesLeaveNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		if err := WriteAuthKey(dir, "p", fmt.Sprint("k", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveTheme(dir, "dark"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "auth.json" && e.Name() != "config.json" {
			t.Errorf("stray file left behind: %s", e.Name())
		}
	}
}

func TestSaveThemeKeepsModeAndSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes and symlinks")
	}
	// A user who manages config.json from dotfiles: the symlink must
	// survive the write, the target gets the new theme, and its mode
	// is not silently changed.
	home := t.TempDir()
	dotfiles := t.TempDir()
	target := filepath.Join(dotfiles, "config.json")
	if err := os.WriteFile(target, []byte(`{"model": "m1", "future_key": [1, 2]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "config.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := SaveTheme(home, "nord"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("config.json symlink was replaced by a regular file (err=%v)", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("target is not valid JSON: %v\n%s", err, data)
	}
	if raw["theme"] != "nord" || raw["model"] != "m1" || raw["future_key"] == nil {
		t.Errorf("target = %v; want theme set and unknown keys preserved", raw)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o644 {
		t.Errorf("target mode = %v, want the original 0644", fi.Mode().Perm())
	}
}

func TestSaveThemeRefusesMalformedConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	bad := []byte(`{"model": `)
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTheme(dir, "dark"); err == nil {
		t.Fatal("SaveTheme over a malformed config.json must fail, not overwrite")
	}
	if got, _ := os.ReadFile(path); string(got) != string(bad) {
		t.Errorf("malformed config.json was modified: %q", got)
	}
}

func TestUserDir(t *testing.T) {
	tests := []struct {
		name      string
		tildeHome string
		home      string
		want      string
		wantErr   bool
		skipOnWin bool
	}{
		{name: "TILDE_HOME wins", tildeHome: "/custom/tilde", home: "/home/u", want: "/custom/tilde"},
		{name: "falls back to ~/.tilde", home: "/home/u", want: "/home/u/.tilde", skipOnWin: true},
		{name: "no home at all is an error", home: "", wantErr: true, skipOnWin: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipOnWin && runtime.GOOS == "windows" {
				t.Skip("HOME semantics differ")
			}
			t.Setenv("TILDE_HOME", tc.tildeHome)
			t.Setenv("HOME", tc.home)
			got, err := UserDir()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("UserDir() = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestIsLocalBaseURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"http://localhost:11434/v1", true},
		{"http://127.0.0.1:8080", true},
		{"http://127.5.5.5/v1", true},
		{"http://[::1]:8080/v1", true},
		{"https://api.openai.com/v1", false},
		{"https://localhost.evil.com/v1", false},   // substring must not count
		{"https://evil.com/localhost", false},      // path must not count
		{"https://user@evil.com#localhost", false}, // fragment must not count
		{"http://localhost@evil.com/", false},      // userinfo is not the host
		{"http://0.0.0.0:8080", false},             // unspecified is not loopback
		{"http://192.168.1.10:8080", false},        // LAN is not local
		{"://bad", false},                          // unparseable fails closed
		{"", false},
	}
	for _, tc := range tests {
		if got := IsLocalBaseURL(tc.url); got != tc.want {
			t.Errorf("IsLocalBaseURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestLoadersRejectBadFilesLoudly(t *testing.T) {
	// Every loader must fail on unparseable or unreadable input rather
	// than guess, and must treat only "does not exist" as empty.
	type loader func(dir string) error
	loaders := map[string]struct {
		file string
		load loader
	}{
		"config": {"config.json", func(d string) error { _, err := LoadConfig(d); return err }},
		"models": {"models.json", func(d string) error { _, err := LoadModels(d); return err }},
		"auth":   {"auth.json", func(d string) error { _, _, err := LoadAuth(d); return err }},
	}
	bad := map[string]string{
		"truncated":       `{"model": "x"`,
		"not json":        `model = x`,
		"wrong top level": `["a", "b"]`,
		"binary":          "\x00\x01\x02\xff",
		"empty file":      ``,
	}
	for lname, l := range loaders {
		for bname, content := range bad {
			t.Run(lname+"/"+bname, func(t *testing.T) {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, l.file), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := l.load(dir); err == nil {
					t.Errorf("%s accepted %s content", lname, bname)
				}
			})
		}
		t.Run(lname+"/is a directory", func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, l.file), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := l.load(dir); err == nil {
				t.Errorf("%s treated a directory as an empty config", lname)
			}
		})
	}
}

// SaveModel and SaveDefaultProvider are the /model picker's memory: the
// choice made in the UI must be what the next launch loads, with every
// unrelated key (including ones tilde does not model) surviving.
func TestSaveModelAndDefaultProviderPersistChoice(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"model":"old-model","theme":"green","future_key":[1,2]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveModel(dir, "mistral-small-latest"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "mistral-small-latest" {
		t.Errorf("LoadConfig model = %q", cfg.Model)
	}
	if cfg.Theme != "green" {
		t.Errorf("unrelated key lost: theme = %q", cfg.Theme)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "future_key") {
		t.Errorf("unknown key lost from config.json: %s", raw)
	}

	// models.json may not exist at all on a first run; the provider is
	// created there and LoadModels must select it.
	if err := SaveDefaultProvider(dir, "mistral"); err != nil {
		t.Fatal(err)
	}
	models, err := LoadModels(dir)
	if err != nil {
		t.Fatal(err)
	}
	if models.DefaultProvider != "mistral" {
		t.Errorf("LoadModels default_provider = %q", models.DefaultProvider)
	}
}
