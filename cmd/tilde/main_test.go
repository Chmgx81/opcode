package main

import (
	"errors"
	"testing"

	"github.com/Chmgx81/tilde/internal/update"
)

// A source/test build reports "(devel)", which `tilde update` must
// refuse before making any network call.
func TestUpdateRefusesDevBuildWithoutNetwork(t *testing.T) {
	if buildVersion() != "(devel)" {
		t.Skipf("test binary reports %q; would reach the real network", buildVersion())
	}
	for _, args := range [][]string{{}, {"--check"}} {
		if err := runUpdate(args); !errors.Is(err, update.ErrDevBuild) {
			t.Errorf("runUpdate(%v) = %v, want ErrDevBuild", args, err)
		}
	}
}

func TestUpdateArgumentErrors(t *testing.T) {
	if err := runUpdate([]string{"--bogus"}); err == nil {
		t.Error("unknown flag accepted")
	}
	if err := runUpdate([]string{"now"}); err == nil {
		t.Error("positional argument accepted")
	}
	if err := runUpdate([]string{"-h"}); err != nil {
		t.Errorf("-h = %v, want nil", err)
	}
}
