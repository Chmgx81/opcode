package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// credEnv builds a TILDE_HOME holding a real auth.json and returns
// the directory and the file.
func credEnv(t *testing.T) (home, cred string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("TILDE_HOME", home)
	cred = filepath.Join(home, "auth.json")
	if err := os.WriteFile(cred, []byte(`{"anthropic":"sk-secret-value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, cred
}

func jsonArgs(t *testing.T, v map[string]any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func denyAll(t *testing.T, name string, calls map[Tool]string) {
	t.Helper()
	for _, mode := range Modes {
		decide := PolicyDecide(mode, func(Tool, string) bool { return true }) // even a user who would approve
		for tool, args := range calls {
			if decide(tool, args) {
				t.Errorf("%s: mode %s allowed %s %s", name, mode, tool.Name(), args)
			}
		}
	}
}

// Every tool that takes a file path is covered, by its real argument
// name — including apply_patch, whose paths live inside the patch text
// — and every way to spell the same file.
func TestCredentialsDeniedForEveryPathTool(t *testing.T) {
	home, cred := credEnv(t)

	t.Run("each tool", func(t *testing.T) {
		p := jsonQuote(cred)
		denyAll(t, "absolute", map[Tool]string{
			ReadFile{}:  `{"path": ` + p + `}`,
			WriteFile{}: `{"path": ` + p + `, "content": "x"}`,
			EditFile{}:  `{"path": ` + p + `, "old": "a", "new": "b"}`,
			Grep{}:      `{"pattern": "sk-", "path": ` + p + `}`,
			Glob{}:      `{"pattern": "*", "path": ` + p + `}`,
		})
		for kind, section := range map[string]string{
			"add":    "*** Add File: " + cred + "\n+x",
			"update": "*** Update File: " + cred + "\n@@\n-a\n+b",
			"delete": "*** Delete File: " + cred,
		} {
			patch := "*** Begin Patch\n" + section + "\n*** End Patch"
			denyAll(t, "apply_patch "+kind, map[Tool]string{
				ApplyPatch{}: jsonArgs(t, map[string]any{"patch": patch}),
			})
		}
		// One innocent file first must not launder a later section.
		mixed := "*** Begin Patch\n*** Add File: ok.txt\n+x\n*** Delete File: " + cred + "\n*** End Patch"
		denyAll(t, "mixed patch", map[Tool]string{
			ApplyPatch{}: jsonArgs(t, map[string]any{"patch": mixed}),
		})
	})

	t.Run("key spelling", func(t *testing.T) {
		// encoding/json matches keys case-insensitively, so the tool
		// itself accepts these — the check must too.
		denyAll(t, "case", map[Tool]string{
			ReadFile{}: `{"PATH": ` + jsonQuote(cred) + `}`,
		})
		denyAll(t, "duplicate key", map[Tool]string{
			ReadFile{}: `{"path": "README.md", "path": ` + jsonQuote(cred) + `}`,
		})
	})

	t.Run("path spelling", func(t *testing.T) {
		denyAll(t, "dotdot", map[Tool]string{
			ReadFile{}: `{"path": ` + jsonQuote(filepath.Join(home, "sub", "..", "auth.json")) + `}`,
		})
		denyAll(t, "dot segments", map[Tool]string{
			ReadFile{}: `{"path": ` + jsonQuote(home+"/./auth.json") + `}`,
		})
		denyAll(t, "double slash", map[Tool]string{
			ReadFile{}: `{"path": ` + jsonQuote(home+"//auth.json") + `}`,
		})
		t.Chdir(home)
		denyAll(t, "relative, cwd is the credentials dir", map[Tool]string{
			ReadFile{}:  `{"path": "auth.json"}`,
			WriteFile{}: `{"path": "./auth.json", "content": "x"}`,
		})
		t.Chdir(filepath.Join(home, ".."))
		denyAll(t, "relative, ../ from a sibling", map[Tool]string{
			ReadFile{}: `{"path": ` + jsonQuote(filepath.Join(filepath.Base(home), "auth.json")) + `}`,
		})
	})
}

// Links to the file are the file: a hard link shares the inode, a
// symlinked home directory changes the spelling of every path, and a
// symlink that dangles into a not-yet-existing credentials file
// would create it on write.
func TestCredentialsDeniedThroughLinks(t *testing.T) {
	t.Run("hard link", func(t *testing.T) {
		_, cred := credEnv(t)
		link := filepath.Join(t.TempDir(), "innocent.txt")
		if err := os.Link(cred, link); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		denyAll(t, "hard link", map[Tool]string{ReadFile{}: `{"path": ` + jsonQuote(link) + `}`})
	})

	// Real systems: /home -> /var/home (Fedora Atomic), a ~/.tilde
	// that is a symlink into a dotfiles repo.
	t.Run("symlinked credentials dir, real path used", func(t *testing.T) {
		real := t.TempDir()
		if err := os.WriteFile(filepath.Join(real, "auth.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		linked := filepath.Join(t.TempDir(), "dotfiles-tilde")
		if err := os.Symlink(real, linked); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Setenv("TILDE_HOME", linked)
		denyAll(t, "real path", map[Tool]string{
			ReadFile{}: `{"path": ` + jsonQuote(filepath.Join(real, "auth.json")) + `}`,
		})
		denyAll(t, "linked path", map[Tool]string{
			ReadFile{}: `{"path": ` + jsonQuote(filepath.Join(linked, "auth.json")) + `}`,
		})
	})

	t.Run("file does not exist yet", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("TILDE_HOME", home)
		cred := filepath.Join(home, "auth.json")
		// A model-created credentials file is tampering too.
		denyAll(t, "create", map[Tool]string{
			WriteFile{}: `{"path": ` + jsonQuote(cred) + `, "content": "{}"}`,
		})
		// Case-folded spelling: conservative (a case-insensitive
		// filesystem would make it the same file).
		denyAll(t, "case-folded", map[Tool]string{
			WriteFile{}: `{"path": ` + jsonQuote(filepath.Join(home, "AUTH.JSON")) + `, "content": "{}"}`,
		})
		link := filepath.Join(t.TempDir(), "dangling")
		if err := os.Symlink(cred, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		denyAll(t, "dangling link", map[Tool]string{
			WriteFile{}: `{"path": ` + jsonQuote(link) + `, "content": "{}"}`,
		})
	})
}

// The tools refuse the file themselves too: a directory walk reaches
// it without naming it, and a gate-time path check leaves a window
// for a link swap.
func TestReadToolsRefuseCredentialsThemselves(t *testing.T) {
	home, cred := credEnv(t)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"note": "sk-visible"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	out, err := (ReadFile{}).Execute(ctx, jsonArgs(t, map[string]any{"path": cred}))
	if !errors.Is(err, errCredentialsFile) || strings.Contains(out+err.Error(), "sk-secret-value") {
		t.Errorf("read_file(auth.json) = %q, %v", out, err)
	}
	link := filepath.Join(t.TempDir(), "l")
	if err := os.Symlink(cred, link); err == nil {
		if _, err := (ReadFile{}).Execute(ctx, jsonArgs(t, map[string]any{"path": link})); !errors.Is(err, errCredentialsFile) {
			t.Errorf("read_file through a symlink: err = %v", err)
		}
	}

	search := func(root string) string {
		t.Helper()
		args := map[string]any{"pattern": "sk-"}
		if root != "" {
			args["path"] = root
		}
		out, err := (Grep{}).Execute(ctx, jsonArgs(t, args))
		if err != nil {
			t.Fatalf("grep: %v", err)
		}
		return out
	}
	for name, out := range map[string]string{
		"rooted at the dir":    search(home),
		"rooted above the dir": search(filepath.Dir(home)),
		"the file itself":      search(cred),
	} {
		if strings.Contains(out, "sk-secret-value") || strings.Contains(out, "auth.json") {
			t.Errorf("grep %s reached the credentials file:\n%s", name, out)
		}
	}
	// Control: the walk itself works, only that file is skipped.
	if out := search(home); !strings.Contains(out, "config.json") {
		t.Errorf("grep no longer finds ordinary files:\n%s", out)
	}
	// And with no path at all, from inside the directory.
	t.Chdir(home)
	if out := search(""); strings.Contains(out, "sk-secret-value") {
		t.Errorf("grep with cwd = credentials dir leaked:\n%s", out)
	}
}

// The credentials file (~/.tilde/auth.json) must be unreachable by
// every tool in every mode (audit S1): read-tier calls run free, so
// without the deny the keys would ride straight into the model's
// context.
func TestCredentialsFileDenied(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TILDE_HOME", home)
	cred := filepath.Join(home, "auth.json")
	if err := os.WriteFile(cred, []byte(`{"anthropic":"sk-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range Modes {
		decide := PolicyDecide(mode, nil)
		for _, tool := range []Tool{ReadFile{}, Grep{}, Glob{}, ListDir{}, EditFile{}} {
			if decide(tool, `{"path": `+jsonQuote(cred)+`}`) {
				t.Errorf("%s allowed %s on auth.json in mode %s", mode, tool.Name(), mode)
			}
		}
	}
}

// The deny follows symlinks: a read of a link pointing at auth.json
// is a read of auth.json.
func TestCredentialsSymlinkDenied(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TILDE_HOME", home)
	cred := filepath.Join(home, "auth.json")
	if err := os.WriteFile(cred, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "innocent.txt")
	if err := os.Symlink(cred, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	decide := PolicyDecide(ModePlan, nil)
	if decide(ReadFile{}, `{"path": `+jsonQuote(link)+`}`) {
		t.Error("allowed read through a symlink to auth.json")
	}
}

// Ordinary reads are untouched: the deny is scoped to the one file,
// not to the whole home directory or the working tree.
func TestCredentialsDenyScoped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TILDE_HOME", home)
	decide := PolicyDecide(ModePlan, nil)
	other := filepath.Join(home, "config.json")
	if !decide(ReadFile{}, `{"path": `+jsonQuote(other)+`}`) {
		t.Error("config.json should stay readable")
	}
	if !decide(ReadFile{}, `{"path": "README.md"}`) {
		t.Error("working-tree reads should stay free")
	}
	// list_dir on ~/.tilde shows the file exists — existence is not
	// the secret, contents are.
	if !decide(ListDir{}, `{"path": `+jsonQuote(home)+`}`) {
		t.Error("listing ~/.tilde should stay allowed")
	}
}

func jsonQuote(s string) string {
	return `"` + s + `"`
}
