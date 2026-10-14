package tun

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"testing"
)

func TestInstallScript(t *testing.T) {
	s := installScript("/opt/My Kumo/mihomo", 1000, "1000", true)
	for _, want := range []string{
		`cat -- '/opt/My Kumo/mihomo' > "$T"`, "/usr/local/lib/kumo/1000",
		`chown root:1000 "$T" && chmod 0750 "$T"`, "setcap cap_net_admin,cap_net_bind_service=+ep",
		"rm -f " + legacyDest, hashCase(),
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q:\n%s", want, s)
		}
	}
	// The check comes before the rights, and the move after both.
	if i, j, k := strings.Index(s, "sha256sum \"$T\""), strings.Index(s, "setcap cap"), strings.Index(s, `mv -f "$T"`); !(i < j && j < k) {
		t.Error("order: check, rights, move")
	}
	if s := installScript("/c", 1000, "100", false); !strings.Contains(s, "setfacl -m u:1000:rx") || !strings.Contains(s, "chmod 0700") {
		t.Errorf("shared group: no ACL:\n%s", s)
	}
	if out, err := exec.Command("sh", "-n", "-c", s).CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v %s", err, out)
	}
}

func exitErr(code int) error {
	return exec.Command("sh", "-c", "exit "+string(rune('0'+code/100))+string(rune('0'+code/10%10))+string(rune('0'+code%10))).Run()
}

func TestPkexecError(t *testing.T) {
	for code, want := range map[int]error{126: ErrDenied, 127: ErrDenied, exitNoTool: ErrNoTool, exitUntrusted: ErrUntrusted, exitNoACL: ErrNoACL} {
		if got := pkexecError(exitErr(code), nil); !errors.Is(got, want) {
			t.Errorf("exit %d: %v, want %v", code, got, want)
		}
	}
	if got := pkexecError(exitErr(exitFailed), []byte("install: boom\n")); got == nil || got.Error() != "install: boom" {
		t.Errorf("exit 5: %v", got)
	}
}

// TestRootInstall runs the install script as root through sudo, as pkexec
// would, against the core named by KUMO_TUN_SRC. It changes the machine,
// so it runs only when asked.
func TestRootInstall(t *testing.T) {
	src := os.Getenv("KUMO_TUN_SRC")
	if src == "" {
		t.Skip("KUMO_TUN_SRC not set")
	}
	u, _ := user.Current()
	run := func(script string) error {
		out, err := exec.Command("sudo", "-n", "/bin/sh", "-c", script).CombinedOutput()
		return pkexecError(err, out)
	}
	fake := t.TempDir() + "/mihomo"
	os.WriteFile(fake, []byte("#!/bin/sh\necho evil\n"), 0o755)
	if err := run(installScript(fake, os.Getuid(), u.Gid, privateGroup(u, u.Gid))); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("an unpinned core: %v, want ErrUntrusted", err)
	}
	if err := run(installScript(src, os.Getuid(), u.Gid, privateGroup(u, u.Gid))); err != nil {
		t.Fatal(err)
	}
	l, ok := Core(src)
	if !ok || l.Binary != Dest() {
		t.Fatalf("Core = %v %v, want %s", l, ok, Dest())
	}
	fi, _ := os.Stat(Dest())
	if fi.Mode().Perm()&0o007 != 0 {
		t.Fatalf("others may run the copy: %v", fi.Mode())
	}
	if _, err := os.Stat(legacyDest); err == nil {
		t.Fatal("the 0.4.3 copy was left")
	}
}
