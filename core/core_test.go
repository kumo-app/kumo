package core

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"kumo/clash"
)

const testProfile = `mixed-port: 0
port: 0
mode: rule
external-controller: 0.0.0.0:9999
secret: theirs
proxies:
  - {name: A, type: socks5, server: 127.0.0.9, port: 1080}
proxy-groups:
  - {name: Pick, type: select, proxies: [A, DIRECT]}
rules:
  - MATCH,Pick
`

func TestRuntime(t *testing.T) {
	risky := testProfile + "allow-lan: true\nbind-address: '*'\nexternal-ui: ui\nexternal-ui-url: http://evil/ui.zip\nexternal-controller-cors: {allow-origins: ['*']}\ntun: {enable: true}\n"
	out, err := Runtime([]byte(risky), Overrides{Controller: "127.0.0.1:1234", Secret: "s", MixedPort: 7899})
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	yaml.Unmarshal(out, &m)
	if m["external-controller"] != "127.0.0.1:1234" || m["secret"] != "s" || m["mixed-port"] != 7899 {
		t.Fatalf("overrides not applied: %v", m)
	}
	for _, k := range []string{"external-ui", "external-ui-url", "external-controller-cors", "bind-address"} {
		if _, ok := m[k]; ok {
			t.Fatalf("%s kept", k)
		}
	}
	if m["allow-lan"] != false || m["tun"].(map[string]any)["enable"] != false {
		t.Fatalf("allow-lan / tun not forced off: %v %v", m["allow-lan"], m["tun"])
	}
	if MixedPort(out) != 7899 {
		t.Fatal("mixed port")
	}
	if _, err := Runtime([]byte(":\n- x"), Overrides{}); err == nil {
		t.Fatal("bad YAML accepted")
	}
}

func TestFetchLimits(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("proxies: []\n"))
		w.Write(make([]byte, MaxProfileSize+10))
	}))
	defer big.Close()
	if _, _, _, err := Fetch(context.Background(), big.URL); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("oversized subscription: %v", err)
	}
	if _, _, _, err := Fetch(context.Background(), "ftp://x/y"); err == nil {
		t.Fatal("non-http link accepted")
	}
}

func TestProfilesConcurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(testProfile)) }))
	defer srv.Close()
	ps := LoadProfiles(t.TempDir())
	var ids []string
	for range 4 {
		p, err := ps.AddRemote(context.Background(), srv.URL, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	done := make(chan bool)
	for _, id := range ids {
		go func() { ps.Update(context.Background(), id); done <- true }()
		go func() { ps.SetActive(id); ps.List(); ps.ActiveYAML(); done <- true }()
	}
	for range 2 * len(ids) {
		<-done
	}
	if len(LoadProfiles(ps.Dir).List()) != 4 {
		t.Fatal("profiles lost")
	}
}

func TestProfiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=10; download=20; total=1000; expire=1893456000")
		w.Header().Set("Content-Disposition", `attachment; filename="Test Sub.yaml"`)
		w.Write([]byte(testProfile))
	}))
	defer srv.Close()
	ps := LoadProfiles(t.TempDir())
	p, err := ps.AddRemote(context.Background(), srv.URL, "", 24)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Test Sub" || p.Total != 1000 || ps.Active() != p.ID {
		t.Fatalf("%+v", p)
	}
	if err := ps.Update(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "mine.yaml")
	os.WriteFile(local, []byte(testProfile), 0o644)
	l, err := ps.AddLocal(local)
	if err != nil || l.Name != "mine" {
		t.Fatal(l, err)
	}
	again := LoadProfiles(ps.Dir)
	if len(again.List()) != 2 || again.Active() != p.ID {
		t.Fatalf("%+v", again)
	}
	again.Remove(p.ID)
	if again.Active() != "" || string(again.ActiveYAML()) != DefaultProfile {
		t.Fatal("remove")
	}
}

// TestManager runs the bundled core, switches its profile over the API and
// stops it. It needs the core: go run ./scripts/fetch-core.
func TestManager(t *testing.T) {
	bin := FindBinary("../resources")
	if bin == "" {
		t.Skip("no mihomo: run go run ./scripts/fetch-core")
	}
	m := &Manager{Binary: bin, Dir: t.TempDir()}
	if !strings.Contains(m.Version(), "Mihomo") {
		t.Fatal(m.Version())
	}
	port, secret := FreePort(0), NewSecret()
	ov := Overrides{Controller: "127.0.0.1:" + strconv.Itoa(port), Secret: secret, MixedPort: FreePort(0)}
	cfg := filepath.Join(m.Dir, "runtime.yaml")
	if err := WriteRuntime(cfg, []byte(DefaultProfile), ov); err != nil {
		t.Fatal(err)
	}
	if err := m.Test(cfg); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(cfg); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	cli := &clash.Client{Host: "127.0.0.1", Port: port, Secret: secret}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for {
		if _, err := cli.Version(ctx); err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("core did not answer")
		}
		time.Sleep(200 * time.Millisecond)
	}
	ov.MixedPort = FreePort(0)
	WriteRuntime(cfg, []byte(strings.Replace(testProfile, "mixed-port: 0", "mixed-port: 0", 1)), ov)
	data, _ := os.ReadFile(cfg)
	if err := cli.ReloadConfigs(ctx, data); err != nil {
		t.Fatal(err)
	}
	px, err := cli.Proxies(ctx)
	if err != nil || px["Pick"].Now == "" {
		t.Fatalf("profile not loaded: %v %v", px["Pick"], err)
	}
	m.Stop()
	if s, _ := m.Status(); s != Stopped {
		t.Fatal(s)
	}
}

func TestRuntimeDropsListeners(t *testing.T) {
	risky := testProfile + `redir-port: 7892
tproxy-port: 7893
listeners: [{name: open, type: mixed, port: 9999, listen: 0.0.0.0}]
tunnels: ["tcp,0.0.0.0:6553,8.8.8.8:53,proxy"]
ss-config: ss://x
vmess-config: vmess://x
tuic-server: {enable: true, listen: 0.0.0.0:443}
dns: {enable: true, listen: 0.0.0.0:53}
Proxy: []
tun: {enable: true, stack: gvisor, device: utun99, route-address: [0.0.0.0/1], include-uid: [0], strict-route: true}
`
	out, err := Runtime([]byte(risky), Overrides{Controller: "127.0.0.1:1", Tun: true})
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	yaml.Unmarshal(out, &m)
	for _, k := range []string{"redir-port", "tproxy-port", "listeners", "tunnels", "ss-config", "vmess-config", "tuic-server"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s kept", k)
		}
	}
	if _, ok := m["dns"].(map[string]any)["listen"]; ok {
		t.Error("dns.listen on 0.0.0.0 kept")
	}
	tun := m["tun"].(map[string]any)
	if tun["enable"] != true || tun["stack"] != "gvisor" || tun["device"] != nil || tun["route-address"] != nil || tun["include-uid"] != nil || tun["strict-route"] != nil {
		t.Errorf("tun not rebuilt: %v", tun)
	}
	if _, ok := m["Proxy"]; ok {
		t.Error("a key mihomo does not know kept")
	}
	out, _ = Runtime([]byte(testProfile+"dns: {enable: true, listen: '127.0.0.1:1053'}\n"), Overrides{})
	m = map[string]any{}
	yaml.Unmarshal(out, &m)
	if m["dns"].(map[string]any)["listen"] != "127.0.0.1:1053" {
		t.Error("loopback dns.listen dropped")
	}
}

func TestValidateFormats(t *testing.T) {
	links := "vmess://eyJhZGQiOiJ4In0=\nss://YWVzOnB3@1.2.3.4:8388#a\ntrojan://pw@host:443#b\n"
	b64 := base64.StdEncoding.EncodeToString([]byte(links))
	for name, data := range map[string]string{"plain": links, "base64": b64, "base64 wrapped": b64[:20] + "\n" + b64[20:]} {
		if err := Validate([]byte(data)); !errors.Is(err, ErrURIList) {
			t.Errorf("%s: %v, want ErrURIList", name, err)
		}
	}
	if err := Validate([]byte("<html>login</html>")); !errors.Is(err, ErrNotClash) {
		t.Errorf("html: %v", err)
	}
	if err := Validate([]byte(testProfile)); err != nil {
		t.Errorf("clash: %v", err)
	}
	if _, _, _, err := Fetch(context.Background(), "ftp://x/y"); !errors.Is(err, ErrBadLink) {
		t.Errorf("ftp: %v", err)
	}
}

func TestCheckRuntime(t *testing.T) {
	out, err := Runtime([]byte(testProfile+"listeners: [{name: x, type: mixed, port: 1, listen: 0.0.0.0}]\ntun: {device: utun9}\n"), Overrides{Controller: "127.0.0.1:1", Tun: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckRuntime(out); err != nil {
		t.Fatalf("Runtime's own output refused: %v", err)
	}
	for name, bad := range map[string]string{
		"listeners": "listeners: []\n", "redir": "redir-port: 7892\n", "dns": "dns: {listen: 0.0.0.0:53}\n",
		"tun device": "tun: {enable: true, device: utun9}\n", "tun route": "tun: {enable: true, route-address: [0.0.0.0/1]}\n",
		"quoted": "\"Listeners\": []\n", "tls": "external-controller-tls: 0.0.0.0:9\n",
	} {
		if CheckRuntime([]byte(testProfile+bad)) == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestCheckPatch(t *testing.T) {
	if err := CheckPatch(map[string]any{"mode": "global", "tun": map[string]any{"enable": true}, "mixed-port": 7890}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]any{
		{"redir-port": 1}, {"tun": map[string]any{"enable": true, "device": "x"}}, {"bind-address": "*"}, {"external-controller": "0.0.0.0:1"},
	} {
		if CheckPatch(bad) == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestTestListener(t *testing.T) {
	prof := strings.Replace(testProfile, "proxy-groups:\n", "proxy-groups:\n  - {name: KUMO-TEST, type: select, proxies: [DIRECT]}\n", 1)
	out, err := Runtime([]byte(prof), Overrides{Controller: "127.0.0.1:1", TestPort: 40123})
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckRuntime(out); err != nil {
		t.Fatalf("own test listener refused: %v\n%s", err, out)
	}
	if n := strings.Count(string(out), "name: KUMO-TEST"); n != 1 {
		t.Fatalf("%d test groups", n)
	}
	for _, bad := range []string{
		"listeners: [{name: kumo-test, type: mixed, port: 40123, listen: 0.0.0.0, proxy: KUMO-TEST, udp: true}]\n",
		"listeners: [{name: kumo-test, type: mixed, port: 40123, listen: 127.0.0.1, proxy: KUMO-TEST, udp: true, users: []}]\n",
		"listeners: [{name: kumo-test, type: tun, port: 40123, listen: 127.0.0.1, proxy: KUMO-TEST, udp: true}]\n",
		"listeners: [{name: kumo-test, type: mixed, port: 40123, listen: 127.0.0.1, proxy: KUMO-TEST, udp: true}, {name: x, type: mixed, port: 1, listen: 127.0.0.1, proxy: KUMO-TEST, udp: true}]\n",
	} {
		if CheckRuntime([]byte(testProfile+bad)) == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
