package session

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Chmgx81/opcode/internal/llm"
)

func TestSaveReplacesExistingFileWithPrivateMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	s := FromHistory("m", "plan", []llm.Message{{Role: "user", Content: "hi"}})
	if err := s.Save(path, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("session mode after overwrite = %v, want 0600 (sessions hold prompts and tool output)", perm)
	}
}

func TestSaveCreatesPrivateDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	dir := filepath.Join(t.TempDir(), "sessions")
	s := FromHistory("m", "plan", []llm.Message{{Role: "user", Content: "hi"}})
	if err := s.Save(filepath.Join(dir, "s.json"), nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("sessions dir mode = %v, want 0700", perm)
	}
}

func TestSaveNeverExposesATornFileToReaders(t *testing.T) {
	// The autosave path rewrites the same session file over and over.
	// A reader (or a crash) in the middle must see the previous
	// complete session or the new one, never a truncated file: that is
	// the difference between /resume working and losing the session.
	path := filepath.Join(t.TempDir(), "s.json")
	seed := FromHistory("m", "plan", []llm.Message{{Role: "user", Content: "seed"}})
	if err := seed.Save(path, nil); err != nil {
		t.Fatal(err)
	}

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
			if _, err := Load(path); err != nil {
				mu.Lock()
				torn = err
				mu.Unlock()
				return
			}
		}
	}()
	big := strings.Repeat("x", 1<<20)
	for i := 0; i < 60; i++ {
		s := FromHistory("m", "plan", []llm.Message{{Role: "user", Content: fmt.Sprint(big, i)}})
		if err := s.Save(path, nil); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if torn != nil {
		t.Fatalf("Load saw a torn session file: %v", torn)
	}
}

func TestSaveLeavesNoTempFilesAndLatestIgnoresNone(t *testing.T) {
	dir := t.TempDir()
	s := FromHistory("m", "plan", []llm.Message{{Role: "user", Content: "hi"}})
	path := filepath.Join(dir, "20260101-000000-aaaa.json")
	for i := 0; i < 3; i++ {
		if err := s.Save(path, nil); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dir = %v, want only the session file", names)
	}
}

func TestLoadRejectsDamagedFiles(t *testing.T) {
	good := FromHistory("m", "plan", []llm.Message{
		{Role: "user", Content: "hello"}, {Role: "assistant", Content: "hi"}})
	dir := t.TempDir()
	goodPath := filepath.Join(dir, "good.json")
	if err := good.Save(goodPath, nil); err != nil {
		t.Fatal(err)
	}
	full, err := os.ReadFile(goodPath)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		content []byte
		wantErr bool
	}{
		{"truncated mid-file", full[:len(full)/2], true},
		{"truncated to one byte", full[:1], true},
		{"empty file", nil, true},
		{"not json", []byte("<html>"), true},
		{"binary", []byte{0x00, 0xff, 0xfe}, true},
		{"wrong shape", []byte(`["a"]`), true},
		{"null is an empty session, not a crash", []byte(`null`), false},
		{"empty object is an empty session", []byte(`{}`), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "x.json")
			if err := os.WriteFile(p, tc.content, 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := Load(p)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Load accepted damaged content, got %+v", s)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if s.Nodes == nil {
				t.Error("Nodes must be non-nil so callers can add to it")
			}
			if h := s.History(); len(h) != 0 {
				t.Errorf("history = %v, want none", h)
			}
		})
	}

	if _, err := Load(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("missing file must be an error")
	}
}

func TestLatestSurfacesACorruptNewestFileInsteadOfSilentlyResumingOlder(t *testing.T) {
	// Silently resuming an older session would hide that the newest one
	// is damaged and let the next autosave bury it.
	dir := t.TempDir()
	old := FromHistory("m", "plan", []llm.Message{{Role: "user", Content: "old"}})
	if err := old.Save(filepath.Join(dir, "20260101-000000-aaaa.json"), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20260102-000000-bbbb.json"), []byte(`{"id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, path, err := Latest(dir)
	if err == nil {
		t.Fatalf("Latest resumed %s past a corrupt newest file: %+v", path, s)
	}
	if !strings.Contains(err.Error(), "bbbb.json") {
		t.Errorf("error should name the damaged file: %v", err)
	}
}

func TestLatestSkipsDirectoriesAndNonSessionFiles(t *testing.T) {
	dir := t.TempDir()
	s := FromHistory("m", "plan", []llm.Message{{Role: "user", Content: "real"}})
	if err := s.Save(filepath.Join(dir, "20260101-000000-aaaa.json"), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "zzz.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zzz.txt"), []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, path, err := Latest(dir)
	if err != nil || got == nil {
		t.Fatalf("Latest: %v %v", got, err)
	}
	if !strings.HasSuffix(path, "aaaa.json") {
		t.Errorf("path = %s", path)
	}
}

func TestHistoryOfACyclicFileTerminates(t *testing.T) {
	// Session files are exported, shared, and hand-edited. A parent
	// cycle that never reaches the root must not hang or exhaust memory
	// when the session is resumed.
	cyc := &Session{
		ID: "c", Root: "r",
		Nodes: map[string]*Node{
			"r": {ID: "r", Role: "user", Content: "root"},
			"a": {ID: "a", Parent: "b", Role: "user", Content: "a"},
			"b": {ID: "b", Parent: "a", Role: "assistant", Content: "b"},
		},
		Active: "a",
	}
	done := make(chan []llm.Message, 1)
	go func() { done <- cyc.History() }()
	select {
	case h := <-done:
		if len(h) > len(cyc.Nodes) {
			t.Errorf("history has %d messages from %d nodes", len(h), len(cyc.Nodes))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("History looped forever on a parent cycle")
	}
}

func TestHistoryToleratesDanglingReferences(t *testing.T) {
	tests := []struct {
		name string
		s    *Session
		want int
	}{
		{"active points nowhere", &Session{Root: "r", Active: "gone",
			Nodes: map[string]*Node{"r": {ID: "r", Role: "user", Content: "x"}}}, 0},
		{"parent points nowhere", &Session{Root: "r", Active: "b",
			Nodes: map[string]*Node{"b": {ID: "b", Parent: "missing", Role: "user", Content: "x"}}}, 1},
		{"no root", &Session{Nodes: map[string]*Node{}}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(tc.s.History()); got != tc.want {
				t.Errorf("History len = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNewIDsAreUniqueAndHex(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		id := newID()
		if len(id) != 12 {
			t.Fatalf("id %q has length %d, want 12 hex chars", id, len(id))
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestSaveRedactsToolCallArguments(t *testing.T) {
	s := FromHistory("m", "plan", []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{
		{ID: "c", Name: "bash", Arguments: `{"command": "curl -H 'Authorization: sk-topsecret-99'"}`}}}})
	path := filepath.Join(t.TempDir(), "s.json")
	if err := s.Save(path, []string{"sk-topsecret-99"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "sk-topsecret-99") {
		t.Errorf("secret leaked through tool-call arguments:\n%s", data)
	}
}
