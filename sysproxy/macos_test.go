package sysproxy

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMac models networksetup for a few services.
type fakeMac struct {
	mu     sync.Mutex
	svcs   []string
	vals   map[string]string // svc|key -> value
	delay  time.Duration
	strict bool // refuse an empty server, as some networksetup versions may
	log    []string
	calls  int
}

func newFakeMac(n int) *fakeMac {
	f := &fakeMac{vals: map[string]string{}, delay: 20 * time.Millisecond}
	for i := range n {
		svc := fmt.Sprintf("Service %d", i)
		f.svcs = append(f.svcs, svc)
		for _, k := range []string{"web", "secure", "socks"} {
			f.vals[svc+"|"+k+"Enabled"] = "No"
		}
		f.vals[svc+"|pacEnabled"] = "No"
		f.vals[svc+"|pacURL"] = "(null)"
		f.vals[svc+"|wpad"] = "Off"
	}
	// The user's own setup on the first service: a PAC file, WPAD, and a
	// corporate proxy for HTTPS.
	s := f.svcs[0]
	f.vals[s+"|pacEnabled"], f.vals[s+"|pacURL"], f.vals[s+"|wpad"] = "Yes", "http://corp/proxy.pac", "On"
	f.vals[s+"|secureEnabled"], f.vals[s+"|secureServer"], f.vals[s+"|securePort"] = "Yes", "corp", "3128"
	return f
}

var verbKind = map[string]string{
	"-getwebproxy": "web", "-getsecurewebproxy": "secure", "-getsocksfirewallproxy": "socks",
	"-setwebproxy": "web", "-setsecurewebproxy": "secure", "-setsocksfirewallproxy": "socks",
	"-setwebproxystate": "web", "-setsecurewebproxystate": "secure", "-setsocksfirewallproxystate": "socks",
}

func yes(s string) string {
	if s == "on" {
		return "Yes"
	}
	return "No"
}

func (f *fakeMac) run(args ...string) (string, error) {
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.log = append(f.log, strings.Join(args, "|"))
	v := func(svc, k string) string { return f.vals[svc+"|"+k] }
	switch verb := args[0]; verb {
	case "-listallnetworkservices":
		return "An asterisk (*) denotes that a network service is disabled.\n" + strings.Join(f.svcs, "\n") + "\n*Disabled\n", nil
	case "-getwebproxy", "-getsecurewebproxy", "-getsocksfirewallproxy":
		k := verbKind[verb]
		return fmt.Sprintf("Enabled: %s\nServer: %s\nPort: %s\nAuthenticated Proxy Enabled: 0\n", v(args[1], k+"Enabled"), v(args[1], k+"Server"), v(args[1], k+"Port")), nil
	case "-setwebproxy", "-setsecurewebproxy", "-setsocksfirewallproxy":
		k := verbKind[verb]
		if f.strict && args[2] == "" {
			return "** Error: invalid server", fmt.Errorf("exit status 4")
		}
		f.vals[args[1]+"|"+k+"Server"], f.vals[args[1]+"|"+k+"Port"], f.vals[args[1]+"|"+k+"Enabled"] = args[2], args[3], "Yes"
	case "-setwebproxystate", "-setsecurewebproxystate", "-setsocksfirewallproxystate":
		f.vals[args[1]+"|"+verbKind[verb]+"Enabled"] = yes(args[2])
	case "-getproxybypassdomains":
		if b := v(args[1], "bypass"); b != "" {
			return strings.ReplaceAll(b, " ", "\n") + "\n", nil
		}
		return "There aren't any bypass domains set on " + args[1] + ".\n", nil
	case "-setproxybypassdomains":
		b := strings.Join(args[2:], " ")
		if b == "Empty" {
			b = ""
		}
		f.vals[args[1]+"|bypass"] = b
	case "-getautoproxyurl":
		return fmt.Sprintf("URL: %s\nEnabled: %s\n", v(args[1], "pacURL"), v(args[1], "pacEnabled")), nil
	case "-setautoproxyurl":
		f.vals[args[1]+"|pacURL"], f.vals[args[1]+"|pacEnabled"] = args[2], "Yes"
	case "-setautoproxystate":
		f.vals[args[1]+"|pacEnabled"] = yes(args[2])
	case "-getproxyautodiscovery":
		return "Auto Proxy Discovery: " + v(args[1], "wpad") + "\n", nil
	case "-setproxyautodiscovery":
		f.vals[args[1]+"|wpad"] = map[string]string{"on": "On", "off": "Off"}[args[2]]
	default:
		return "", fmt.Errorf("unknown verb %s", verb)
	}
	return "", nil
}

func (f *fakeMac) snapshotVals() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.vals {
		out[k] = v
	}
	return out
}

func TestMacSetRestore(t *testing.T) {
	f := newFakeMac(8)
	m := macProxy{ns: f.run}
	before := f.snapshotVals()
	st, err := m.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.set("127.0.0.1", 7890); err != nil {
		t.Fatal(err)
	}
	s0 := f.svcs[0]
	if f.vals[s0+"|pacEnabled"] != "No" || f.vals[s0+"|wpad"] != "Off" {
		t.Fatal("PAC / WPAD left on while Kumo's proxy is set")
	}
	if f.vals[s0+"|webServer"] != "127.0.0.1" || f.vals[s0+"|webEnabled"] != "Yes" {
		t.Fatal("proxy not set")
	}
	start := time.Now()
	if err := m.restore(st); err != nil {
		t.Fatal(err)
	}
	// 8 services x 13 calls at 20 ms would take 2 s one after another.
	if d := time.Since(start); d > time.Second {
		t.Errorf("restore took %v: services are not handled in parallel", d)
	}
	after := f.snapshotVals()
	for k, v := range before {
		if strings.HasSuffix(k, "Server") || strings.HasSuffix(k, "Port") {
			continue
		}
		if after[k] != v {
			t.Errorf("%s: %q, want %q", k, after[k], v)
		}
	}
	// Kumo's address is not left behind where the user had none.
	if after[s0+"|webServer"] == "127.0.0.1" || after[f.svcs[1]+"|socksServer"] == "127.0.0.1" {
		t.Error("Kumo's server left in place")
	}
	if after[s0+"|secureServer"] != "corp" || after[s0+"|securePort"] != "3128" || after[s0+"|pacURL"] != "http://corp/proxy.pac" {
		t.Error("the user's own proxy not restored")
	}
}

func TestMacClearOursOnly(t *testing.T) {
	f := newFakeMac(2)
	f.delay = 0
	m := macProxy{ns: f.run}
	m.set("127.0.0.1", 7890)
	s1 := f.svcs[1]
	f.vals[s1+"|socksServer"], f.vals[s1+"|socksPort"] = "10.0.0.1", "1080" // the user's own
	if err := m.clearOurs("127.0.0.1", 7890); err != nil {
		t.Fatal(err)
	}
	if f.vals[f.svcs[0]+"|webEnabled"] != "No" {
		t.Error("Kumo's proxy left on")
	}
	if f.vals[s1+"|socksEnabled"] != "Yes" {
		t.Error("the user's own proxy turned off")
	}
}

// Restoring a service whose proxy had no server clears Kumo's address with
// an empty server, and when networksetup refuses that, still turns the
// proxy off and reports success.
func TestMacRestoreEmptyServer(t *testing.T) {
	for _, strict := range []bool{false, true} {
		f := newFakeMac(2)
		f.delay, f.strict = 0, strict
		m := macProxy{ns: f.run}
		st, err := m.snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if err := m.set("127.0.0.1", 7890); err != nil {
			t.Fatal(err)
		}
		f.log = nil
		if err := m.restore(st); err != nil {
			t.Fatalf("strict=%v: %v", strict, err)
		}
		s1 := f.svcs[1]
		joined := strings.Join(f.log, "\n")
		for _, want := range []string{"-setwebproxy|" + s1 + "||0", "-setwebproxystate|" + s1 + "|off", "-setsocksfirewallproxystate|" + s1 + "|off"} {
			if !strings.Contains(joined, want) {
				t.Errorf("strict=%v: no %q in\n%s", strict, want, joined)
			}
		}
		if i, j := strings.Index(joined, "-setwebproxy|"+s1+"||0"), strings.Index(joined, "-setwebproxystate|"+s1+"|off"); j < i {
			t.Errorf("strict=%v: the state must be set after the server", strict)
		}
		if f.vals[s1+"|webEnabled"] != "No" {
			t.Errorf("strict=%v: proxy left on", strict)
		}
		if !strict && f.vals[s1+"|webServer"] != "" {
			t.Errorf("Kumo's server left: %q", f.vals[s1+"|webServer"])
		}
	}
}
