package sysproxy

import (
	"encoding/base64"
	"testing"
)

const scutilOurs = `<dictionary> {
  ExceptionsList : <array> {
    0 : 127.0.0.1
    1 : *.local
  }
  FTPPassive : 1
  HTTPEnable : 1
  HTTPPort : 7890
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7890
  HTTPSProxy : 127.0.0.1
  SOCKSEnable : 1
  SOCKSPort : 7890
  SOCKSProxy : 127.0.0.1
  __SCOPED__ : <dictionary> {
    en0 : <dictionary> {
      HTTPEnable : 1
      HTTPPort : 8080
      HTTPProxy : 10.0.0.1
    }
  }
}`

func TestScutil(t *testing.T) {
	e := parseScutil(scutilOurs)
	if e.HTTP != "127.0.0.1:7890" || e.HTTPS != "127.0.0.1:7890" || e.SOCKS != "127.0.0.1:7890" || e.PAC != "" || e.AutoDetect {
		t.Fatalf("%+v", e)
	}
	if c, _ := e.Check(7890); c != Applied {
		t.Fatalf("cause %v", c)
	}
	if c, _ := e.Check(7891); c != Elsewhere {
		t.Fatalf("another port: %v", c)
	}
	off := parseScutil("<dictionary> {\n  HTTPEnable : 0\n  HTTPSEnable : 0\n}")
	if c, _ := off.Check(7890); c != Off {
		t.Fatalf("off: %v", c)
	}
	pac := parseScutil("<dictionary> {\n  HTTPEnable : 1\n  HTTPPort : 7890\n  HTTPProxy : 127.0.0.1\n  ProxyAutoConfigEnable : 1\n  ProxyAutoConfigURLString : http://wpad.corp/proxy.pac\n}")
	if c, d := pac.Check(7890); c != PAC || d != "http://wpad.corp/proxy.pac" {
		t.Fatalf("pac: %v %q", c, d)
	}
	wpad := parseScutil("<dictionary> {\n  ProxyAutoDiscoveryEnable : 1\n}")
	if c, _ := wpad.Check(7890); c != AutoDetect {
		t.Fatalf("wpad: %v", c)
	}
	partial := parseScutil("<dictionary> {\n  HTTPEnable : 1\n  HTTPPort : 7890\n  HTTPProxy : localhost\n}")
	if c, _ := partial.Check(7890); c != Partial {
		t.Fatalf("partial: %v", c)
	}
}

func TestWinINet(t *testing.T) {
	blob := make([]byte, 16)
	blob[8] = 0x01 | flagManual
	e := parseWinINet(true, "127.0.0.1:7890", "", blob)
	if c, _ := e.Check(7890); c != Applied {
		t.Fatalf("%+v", e)
	}
	e = parseWinINet(true, "http=127.0.0.1:7890;https=127.0.0.1:7890;socks=127.0.0.1:7891", "", nil)
	if c, a := e.Check(7890); c != Elsewhere || a != "127.0.0.1:7891" {
		t.Fatalf("per protocol: %v %q", c, a)
	}
	// The blob, when there, says what Windows applies.
	blob[8] = 0x01
	if c, _ := parseWinINet(true, "127.0.0.1:7890", "", blob).Check(7890); c != Off {
		t.Fatalf("blob off: %v", c)
	}
	blob[8] = 0x01 | flagManual | winAutoURL
	if c, _ := parseWinINet(true, "127.0.0.1:7890", "http://x/p.pac", blob).Check(7890); c != PAC {
		t.Fatalf("pac: %v", c)
	}
	blob[8] = 0x01 | flagManual | winAutoDetect
	if c, _ := parseWinINet(true, "127.0.0.1:7890", "", blob).Check(7890); c != AutoDetect {
		t.Fatalf("detect: %v", c)
	}
	if c, _ := parseWinINet(false, "127.0.0.1:7890", "", nil).Check(7890); c != Off {
		t.Fatalf("disabled: %v", c)
	}
}

func TestGnomeAndEnv(t *testing.T) {
	vals := map[string]string{
		"org.gnome.system.proxy mode":       "'manual'",
		"org.gnome.system.proxy.http host":  "'127.0.0.1'",
		"org.gnome.system.proxy.http port":  "7890",
		"org.gnome.system.proxy.https host": "'127.0.0.1'",
		"org.gnome.system.proxy.https port": "7890",
	}
	get := func(s, k string) string { return vals[s+" "+k] }
	if c, _ := parseGnome(get).Check(7890); c != Applied {
		t.Fatal("gnome manual")
	}
	vals["org.gnome.system.proxy mode"] = "'auto'"
	if c, _ := parseGnome(get).Check(7890); c != AutoDetect {
		t.Fatal("gnome auto without url")
	}
	vals["org.gnome.system.proxy autoconfig-url"] = "'http://x/p.pac'"
	if c, _ := parseGnome(get).Check(7890); c != PAC {
		t.Fatal("gnome pac")
	}
	vals["org.gnome.system.proxy mode"] = "'none'"
	if c, _ := parseGnome(get).Check(7890); c != Off {
		t.Fatal("gnome none")
	}
	env := map[string]string{"all_proxy": "socks5://user:pw@127.0.0.1:7890/"}
	if c, _ := parseEnv(func(k string) string { return env[k] }).Check(7890); c != Applied {
		t.Fatal("env")
	}
}

func TestOffWhereDead(t *testing.T) {
	alive := func(port int) bool { return port == 8888 }
	mac := State{Values: map[string]string{
		"services":                      "Wi-Fi",
		"Wi-Fi\t-getwebproxy":           "Yes\t127.0.0.1\t7897",
		"Wi-Fi\t-getsecurewebproxy":     "Yes\t127.0.0.1\t8888",
		"Wi-Fi\t-getsocksfirewallproxy": "Yes\t10.0.0.1\t1080",
		"Wi-Fi\tpac":                    "Yes\thttp://127.0.0.1:7897/pac",
	}}
	got, changed := mac.OffWhereDead(alive)
	if !changed || got.Values["Wi-Fi\t-getwebproxy"] != "No\t127.0.0.1\t7897" ||
		got.Values["Wi-Fi\t-getsecurewebproxy"] != "Yes\t127.0.0.1\t8888" ||
		got.Values["Wi-Fi\t-getsocksfirewallproxy"] != "Yes\t10.0.0.1\t1080" ||
		got.Values["Wi-Fi\tpac"] != "No\thttp://127.0.0.1:7897/pac" {
		t.Fatalf("mac %v", got.Values)
	}
	if mac.Values["Wi-Fi\t-getwebproxy"] != "Yes\t127.0.0.1\t7897" {
		t.Fatal("the backup itself changed")
	}
	blob := make([]byte, 12)
	blob[8] = 0x01 | flagManual | winAutoURL
	win := State{Values: map[string]string{"ProxyEnable": "1", "ProxyServer": "127.0.0.1:7897", "AutoConfigURL": "http://localhost:7897/p.pac",
		"DefaultConnectionSettings": base64.StdEncoding.EncodeToString(blob)}}
	got, changed = win.OffWhereDead(alive)
	b, _ := base64.StdEncoding.DecodeString(got.Values["DefaultConnectionSettings"])
	if !changed || got.Values["ProxyEnable"] != "0" || got.Values["AutoConfigURL"] != "" || b[8] != 0x01 {
		t.Fatalf("windows %v %x", got.Values, b[8])
	}
	if _, ok := got.Values["AutoConfigURL"]; ok {
		t.Fatal("a dead PAC must be removed")
	}
	gn := State{Values: map[string]string{"org.gnome.system.proxy mode": "'manual'", "org.gnome.system.proxy.http host": "'127.0.0.1'", "org.gnome.system.proxy.http port": "7897"}}
	if got, changed = gn.OffWhereDead(alive); !changed || got.Values["org.gnome.system.proxy mode"] != "'none'" {
		t.Fatalf("gnome %v", got.Values)
	}
	// A live local proxy, and the user's remote one, are restored as they were.
	keep := State{Values: map[string]string{"ProxyEnable": "1", "ProxyServer": "127.0.0.1:8888"}}
	if _, changed = keep.OffWhereDead(alive); changed {
		t.Fatal("a live proxy was turned off")
	}
	remote := State{Values: map[string]string{"ProxyEnable": "1", "ProxyServer": "proxy.corp:3128"}}
	if _, changed = remote.OffWhereDead(alive); changed {
		t.Fatal("a remote proxy was turned off")
	}
}
