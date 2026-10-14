package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultProfile is the configuration the core runs with before any
// profile is added: everything direct, so that the app works at once.
const DefaultProfile = `# Kumo's default profile: no proxies, every connection goes direct.
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
ipv6: true
proxies: []
proxy-groups:
  - name: Proxy
    type: select
    proxies: [DIRECT]
rules:
  - MATCH,DIRECT
`

// Overrides are what Kumo sets in every profile it runs.
type Overrides struct {
	// Controller is host:port of external-controller, Secret its secret.
	Controller string
	Secret     string
	// MixedPort is used when the profile opens no port of its own.
	MixedPort int
	// Tun is TUN mode, which only Kumo decides.
	Tun bool
	// AllowLan opens the proxy to the local network, which only Kumo
	// decides.
	AllowLan bool
	// TestPort, when set, opens Kumo's node test listener on loopback:
	// see TestGroup.
	TestPort int
}

// TestGroup and TestListener are how Kumo tests one node (download speed,
// UDP) without touching the user's choice: a hidden selector of every
// node, and a mixed listener on 127.0.0.1 that always goes through it.
const (
	TestGroup    = "KUMO-TEST"
	TestListener = "kumo-test"
)

func testListener(port int) map[string]any {
	return map[string]any{"name": TestListener, "type": "mixed", "port": port, "listen": "127.0.0.1", "proxy": TestGroup, "udp": true}
}

// Runtime merges the overrides into a profile, giving the configuration
// the core runs with.
func Runtime(profile []byte, ov Overrides) ([]byte, error) {
	m := map[string]any{}
	if err := yaml.Unmarshal(profile, &m); err != nil {
		return nil, fmt.Errorf("profile is not valid YAML: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}
	m["external-controller"] = ov.Controller
	m["secret"] = ov.Secret
	// Kumo is the dashboard; TLS and unix sockets of the controller would
	// only open other doors.
	delete(m, "external-controller-tls")
	delete(m, "external-controller-unix")
	delete(m, "external-controller-pipe")
	// A subscription must not open the core to others or fetch a web UI.
	for _, k := range []string{"external-ui", "external-ui-url", "external-ui-name", "external-controller-cors", "external-doh-server"} {
		delete(m, k)
	}
	m["allow-lan"] = ov.AllowLan
	if !ov.AllowLan {
		delete(m, "bind-address")
		delete(m, "lan-allowed-ips")
	}
	open := false
	for _, k := range []string{"mixed-port", "port", "socks-port"} {
		if portOf(m[k]) > 0 {
			open = true
		}
	}
	if !open && ov.MixedPort > 0 {
		m["mixed-port"] = ov.MixedPort
	}
	// Nothing but the ports above may listen: a subscription must not turn
	// the computer into a proxy or resolver for the network around it.
	for _, k := range inbounds {
		delete(m, k)
	}
	if dns, ok := m["dns"].(map[string]any); ok {
		if l, _ := dns["listen"].(string); !loopback(l) {
			delete(dns, "listen")
		}
	}
	m["tun"] = tunBlock(m["tun"], ov.Tun)
	if ov.TestPort > 0 {
		groups, _ := m["proxy-groups"].([]any)
		groups = slices.DeleteFunc(slices.Clone(groups), func(g any) bool {
			gm, _ := g.(map[string]any)
			return gm["name"] == TestGroup
		})
		m["proxy-groups"] = append(groups, map[string]any{"name": TestGroup, "type": "select", "include-all": true, "hidden": true})
		m["listeners"] = []any{testListener(ov.TestPort)}
	}
	// mihomo's keys are lowercase; the macOS helper refuses others.
	for k := range m {
		if k == "" || k[0] < 'a' || k[0] > 'z' {
			delete(m, k)
		}
	}
	return yaml.Marshal(m)
}

// inbounds are the keys besides mixed-port, port and socks-port that open
// a listener; Kumo drops them all.
var inbounds = []string{"listeners", "tunnels", "redir-port", "tproxy-port", "ss-config", "vmess-config", "tuic-server", "shadowsocks-config"}

// tunBlock is the tun section Kumo runs: its own, keeping from the
// profile only the network stack. Device names, routes, uid filters and
// hijacks of a subscription do not reach a core that may run as root.
func tunBlock(profile any, enable bool) map[string]any {
	stack := "mixed"
	if t, ok := profile.(map[string]any); ok {
		if s, _ := t["stack"].(string); slices.Contains([]string{"system", "gvisor", "mixed"}, strings.ToLower(s)) {
			stack = strings.ToLower(s)
		}
	}
	return map[string]any{
		"enable": enable, "stack": stack,
		"auto-route": true, "auto-detect-interface": true,
		"dns-hijack": []string{"any:53"},
	}
}

// loopback says whether host:port listens on the loopback interface only.
func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// WriteRuntime writes the merged configuration to path.
func WriteRuntime(path string, profile []byte, ov Overrides) error {
	data, err := Runtime(profile, ov)
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, data, 0o600)
}

// portOf reads a port written as a number or a string.
func portOf(v any) int {
	switch v := v.(type) {
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	}
	return 0
}

// MixedPort returns the port proxies are served on in a runtime
// configuration: mixed-port, else port.
func MixedPort(runtime []byte) int {
	m := map[string]any{}
	yaml.Unmarshal(runtime, &m)
	for _, k := range []string{"mixed-port", "port"} {
		if n := portOf(m[k]); n > 0 {
			return n
		}
	}
	return 0
}

// FreePort returns preferred when it can be listened on at 127.0.0.1, else
// a port the system picks.
func FreePort(preferred int) int {
	if preferred > 0 {
		if l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(preferred)); err == nil {
			l.Close()
			return preferred
		}
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 9097
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// NewSecret is a random secret for the controller.
func NewSecret() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// CheckRuntime says whether data is a configuration Runtime could have
// produced, as far as what it opens is concerned: no inbound but the
// proxy ports, no controller keys, DNS on loopback only, Kumo's own tun
// block, and lowercase keys. The macOS Enhanced Mode helper checks every
// configuration a privileged core is asked to load against it.
func CheckRuntime(data []byte) error {
	m := map[string]any{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("not valid YAML: %w", err)
	}
	for k := range m {
		if k == "" || k[0] < 'a' || k[0] > 'z' {
			return fmt.Errorf("unexpected key %q", k)
		}
	}
	if l, ok := m["listeners"]; ok {
		if err := checkListeners(l); err != nil {
			return err
		}
	}
	for _, k := range append(slices.DeleteFunc(slices.Clone(inbounds), func(k string) bool { return k == "listeners" }), "external-controller-tls", "external-controller-unix", "external-controller-pipe", "external-doh-server", "external-ui", "external-ui-url", "external-ui-name", "external-controller-cors") {
		if _, ok := m[k]; ok {
			return fmt.Errorf("%s is not allowed", k)
		}
	}
	if dns, ok := m["dns"].(map[string]any); ok {
		if l, ok := dns["listen"]; ok {
			if s, _ := l.(string); !loopback(s) {
				return fmt.Errorf("dns.listen %v is not on loopback", l)
			}
		}
	}
	if t, ok := m["tun"]; ok {
		if err := checkTun(t, true); err != nil {
			return err
		}
	}
	return nil
}

// checkTun accepts a tun block with Kumo's keys and values only; with
// whole, every routing key must be present as Kumo writes it.
func checkTun(v any, whole bool) error {
	t, ok := v.(map[string]any)
	if !ok {
		return errors.New("tun is not a mapping")
	}
	for k, val := range t {
		switch k {
		case "enable":
			if _, ok := val.(bool); !ok {
				return errors.New("tun.enable is not a boolean")
			}
		case "stack":
			if s, _ := val.(string); !slices.Contains([]string{"system", "gvisor", "mixed"}, s) {
				return fmt.Errorf("tun.stack %v", val)
			}
		case "auto-route", "auto-detect-interface":
			if val != true {
				return fmt.Errorf("tun.%s must be true", k)
			}
		case "dns-hijack":
			l, _ := val.([]any)
			if len(l) != 1 || l[0] != "any:53" {
				return fmt.Errorf("tun.dns-hijack %v", val)
			}
		default:
			return fmt.Errorf("tun.%s is not allowed", k)
		}
	}
	return nil
}

// patchable are the keys of PATCH /configs Kumo uses.
var patchable = []string{"mode", "log-level", "allow-lan", "ipv6", "mixed-port", "port", "socks-port", "tun"}

// CheckPatch says whether a PATCH /configs body only changes what Kumo
// changes: the mode, log level, LAN access, IPv6, the proxy ports, and
// whether TUN is on.
func CheckPatch(body map[string]any) error {
	for k, v := range body {
		if !slices.Contains(patchable, k) {
			return fmt.Errorf("%s may not be changed", k)
		}
		if k == "tun" {
			t, ok := v.(map[string]any)
			if !ok || len(t) != 1 {
				return errors.New("only tun.enable may be changed")
			}
			if _, ok := t["enable"].(bool); !ok {
				return errors.New("only tun.enable may be changed")
			}
		}
	}
	return nil
}

// checkListeners allows Kumo's test listener only, exactly as Runtime
// writes it.
func checkListeners(v any) error {
	l, ok := v.([]any)
	if !ok || len(l) != 1 {
		return errors.New("listeners are not allowed")
	}
	got, ok := l[0].(map[string]any)
	if !ok {
		return errors.New("listeners are not allowed")
	}
	port, ok := got["port"].(int)
	if !ok || port < 1024 || port > 65535 {
		return errors.New("listeners are not allowed")
	}
	want := testListener(port)
	if len(got) != len(want) {
		return errors.New("listeners are not allowed")
	}
	for k, v := range want {
		if got[k] != v {
			return errors.New("listeners are not allowed")
		}
	}
	return nil
}
