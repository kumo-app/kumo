package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"kumo/core"
)

func TestExplainSubErrorTyped(t *testing.T) {
	lang = "en"
	if !strings.Contains(explainSubError(core.ErrURIList), "share links") {
		t.Error("URI list")
	}
	if !strings.Contains(explainSubError(errors.Join(errors.New("x"), core.ErrNotClash)), "flag=clash") {
		t.Error("not clash")
	}
}

// TestReclaim needs files the user does not own, made with sudo.
func TestReclaim(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 || exec.Command("sudo", "-n", "true").Run() != nil {
		t.Skip("needs sudo")
	}
	dir := t.TempDir()
	mine := filepath.Join(dir, "config.yaml")
	os.WriteFile(mine, []byte("x"), 0o600)
	for _, cmd := range [][]string{
		{"touch", filepath.Join(dir, "cache.db")},
		{"mkdir", "-p", filepath.Join(dir, "providers", "sub")},
		{"chown", "-R", "0:0", filepath.Join(dir, "cache.db"), filepath.Join(dir, "providers")},
	} {
		if out, err := exec.Command("sudo", append([]string{"-n"}, cmd...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v %s", cmd, err, out)
		}
	}
	t.Cleanup(func() { exec.Command("sudo", "-n", "rm", "-rf", dir).Run() })
	reclaim(dir)
	if _, err := os.Stat(filepath.Join(dir, "cache.db")); err == nil {
		t.Error("root's cache.db kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "providers")); err == nil {
		t.Error("root's providers folder kept in place")
	}
	if _, err := os.Stat(mine); err != nil {
		t.Error("the user's own file removed")
	}
}

// Escape (the cancel button) on the reauthorize dialog must mean Later,
// never Turn off.
func TestReauthEscapeIsLater(t *testing.T) {
	b := reauthButtons()
	if len(b) != 3 || b[reauthLater] != tr("Later") || b[reauthTurnOff] != tr("Turn off Enhanced Mode") || b[reauthSetUp] != tr("Set up again") {
		t.Fatalf("buttons %v", b)
	}
}
