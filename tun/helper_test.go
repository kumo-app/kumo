//go:build unix

package tun

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testPaths(t *testing.T) helperPaths {
	p := helperPaths{root: t.TempDir(), uid: os.Getuid()}
	for _, d := range []string{p.home(), p.logs(), p.users()} {
		os.MkdirAll(d, 0o700)
	}
	return p
}

func TestHelperScriptParses(t *testing.T) {
	p := helperPaths{root: "/Library/Application Support/Kumo", uid: 501}
	s := helperScript(p, "0123abcd")
	if out, err := exec.Command("sh", "-n", "-c", s).CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v %s", err, out)
	}
	for _, want := range []string{
		`sudo -n -u "#$U" /bin/cat`, `-ext-ctl-unix "$H/ctl.sock"`, `KUMO_FILTER_LISTEN="127.0.0.1:$port"`, `"$R/kumo-filter"`,
		"C='0123abcd'", `codesign --verify -R="cdhash H\"$C\"" "$1"`, `signed "$pid"`, "env -i", `-d "$H"`,
		"'/Library/Application Support/Kumo/home/501'",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("helper lacks %q", want)
		}
	}
	if strings.Contains(s, `-ext-ctl "`) {
		t.Error("the core still gets a TCP controller")
	}
	pl := plist(p)
	for _, want := range []string{"<string>app.kumo.tun.501</string>", "/Library/Application Support/Kumo/users/501/request", "/Library/Application Support/Kumo/helper-501.sh"} {
		if !strings.Contains(pl, want) {
			t.Errorf("plist lacks %q", want)
		}
	}
}

func TestControllerPort(t *testing.T) {
	for cfg, want := range map[string]int{
		"external-controller: 127.0.0.1:9097\n": 9097,
		"external-controller: 0.0.0.0:9097\n":   0,
		"mixed-port: 1\n":                       0,
	} {
		if got := controllerPort([]byte(cfg)); got != want {
			t.Errorf("%q: %d, want %d", cfg, got, want)
		}
	}
}

// fakeHelper answers requests the way helperScript does, without root.
func fakeHelper(t *testing.T, p helperPaths, done <-chan struct{}) {
	last := ""
	for {
		select {
		case <-done:
			return
		case <-time.After(20 * time.Millisecond):
		}
		data, _ := os.ReadFile(p.request())
		req := string(data)
		if req == last {
			continue
		}
		last = req
		f := strings.Fields(req)
		switch {
		case len(f) == 4 && f[0] == "start":
			cfg, _ := os.ReadFile(p.config())
			os.WriteFile(p.log(), []byte("core started with "+fmt.Sprint(len(cfg))+" bytes\n"), 0o600)
			os.WriteFile(p.state(), []byte("running "+f[2]+"\n"), 0o600)
		case len(f) == 2 && f[0] == "stop":
			os.WriteFile(p.state(), []byte("stopped "+f[1]+" 0\n"), 0o600)
		}
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestClientSession(t *testing.T) {
	poll = 10 * time.Millisecond
	p := testPaths(t)
	cfg := filepath.Join(t.TempDir(), "runtime.yaml")
	os.WriteFile(cfg, []byte("external-controller: 127.0.0.1:9097\n"), 0o600)
	done := make(chan struct{})
	defer close(done)
	go fakeHelper(t, p, done)

	sig := make(chan os.Signal, 1)
	var out, errb syncBuf
	var wg sync.WaitGroup
	code := -1
	wg.Go(func() { code = session(p, []byte("external-controller: 127.0.0.1:9097\n"), 9097, sig, &out, &errb) })
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(out.String(), "core started") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if req, _ := os.ReadFile(p.request()); !strings.HasPrefix(string(req), fmt.Sprintf("start %d ", os.Getpid())) {
		t.Fatalf("request %q", req)
	}
	sig <- os.Interrupt
	wg.Wait()
	if code != 0 {
		t.Fatalf("code %d, stderr %s", code, errb.String())
	}
	if w, _, _ := readState(p); w != "stopped" {
		t.Fatalf("state %q after stop", w)
	}
}

func TestClientNoHelper(t *testing.T) {
	poll, answerWait = 10*time.Millisecond, 200*time.Millisecond
	defer func() { answerWait = 10 * time.Second }()
	p := testPaths(t)
	var errb bytes.Buffer
	if code := session(p, nil, 1, make(chan os.Signal), &bytes.Buffer{}, &errb); code != 1 || !strings.Contains(errb.String(), "did not answer") {
		t.Fatalf("code %d, %q", code, errb.String())
	}
}

// TestWriteHelper writes the helper program for a root of the caller's
// choice, for the end-to-end check in scripts/helper-test.sh.
func TestWriteHelper(t *testing.T) {
	root := os.Getenv("KUMO_HELPER_ROOT")
	if root == "" {
		t.Skip("KUMO_HELPER_ROOT not set")
	}
	p := helperPaths{root: root, uid: os.Getuid()}
	if err := os.WriteFile(os.Getenv("KUMO_HELPER_OUT"), []byte(helperScript(p, "")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseCDHash(t *testing.T) {
	out := "Executable=/Applications/Kumo.app/Contents/MacOS/kumo\nIdentifier=kumo\nCDHash=9f2c1e0a7b\nSignature=adhoc\n"
	if h := parseCDHash(out); h != "9f2c1e0a7b" {
		t.Fatalf("got %q", h)
	}
	for _, bad := range []string{"code object is not signed at all", "CDHash=9f\"; rm -rf /"} {
		if h := parseCDHash(bad); h != "" {
			t.Errorf("%q gave %q", bad, h)
		}
	}
}
