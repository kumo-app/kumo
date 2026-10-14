package tun

import (
	"os/exec"
	"strings"
	"testing"
)

func TestAppleScriptEscapes(t *testing.T) {
	s := appleScript(`install "a" b\c`, `say "hi"`)
	if !strings.Contains(s, `install \"a\" b\\c`) || !strings.Contains(s, `with prompt "say \"hi\""`) || !strings.HasSuffix(s, "with administrator privileges") {
		t.Fatal(s)
	}
}

func TestDarwinInstallScript(t *testing.T) {
	p := helperPaths{root: Root, uid: 501}
	s := installScript("/Applications/Kumo.app/Contents/Resources/core/mihomo", p, "20")
	for _, want := range []string{"shasum -a 256", hashCase(), "launchctl bootstrap system", "-m 0700 '/Library/Application Support/Kumo/home/501'", "-o 501 -g 20 -m 0700"} {
		if !strings.Contains(s, want) {
			t.Errorf("lacks %q", want)
		}
	}
	if i, j := strings.Index(s, "shasum"), strings.Index(s, `mv -f "$T"`); i < 0 || j < i {
		t.Error("the copy must be checked before it is moved into place")
	}
	f := installScriptFor("/c/mihomo", "/Applications/Kumo.app/Contents/MacOS/kumo", "abc123", "cd01", p, "20")
	for _, want := range []string{`cat -- '/Applications/Kumo.app/Contents/MacOS/kumo'`, `= 'abc123' ]`, `"$R/kumo-filter"`} {
		if !strings.Contains(f, want) {
			t.Errorf("filter install lacks %q", want)
		}
	}
	if i, j := strings.Index(f, `= 'abc123' ]`), strings.Index(f, `"$R/kumo-filter"`); j < i {
		t.Error("the filter copy must be checked before it is moved into place")
	}
	if strings.Contains(s, "4755") || strings.Contains(s, "u+s") {
		t.Error("setuid")
	}
	if out, err := exec.Command("sh", "-n", "-c", s).CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v %s", err, out)
	}
}
