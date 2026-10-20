package main

import (
	"errors"
	"strings"
	"testing"

	"kumo/clash"
	"kumo/sysproxy"
	"kumo/tun"
)

func menuKumo() *Kumo {
	k := newKumo(&Prefs{}, "", nil, nil)
	k.online, k.proxyOn = true, true
	k.cfg.Mode = "rule"
	k.cfg.Tun.Enable = true
	k.proxies = map[string]clash.Proxy{
		"Proxy":  {Name: "Proxy", Type: "Selector", All: []string{"Auto", "SG"}, Now: "Auto"},
		"Auto":   {Name: "Auto", Type: "URLTest", All: []string{"SG"}, Now: "SG"},
		"GLOBAL": {Name: "GLOBAL", Type: "Selector", All: []string{"Proxy", "DIRECT"}, Now: "DIRECT"},
		"SG":     {Name: "SG", Type: "Http", History: []clash.Delay{{Delay: 42}}},
	}
	k.groups = []string{"Proxy", "Auto", "GLOBAL"}
	return k
}

func find(items []entry, prefix string) (entry, bool) {
	for _, e := range items {
		if strings.HasPrefix(e.label, prefix) {
			return e, true
		}
	}
	return entry{}, false
}

func TestQuickMenuState(t *testing.T) {
	lang = "en"
	k := menuKumo()
	items := k.quickMenu(true)
	if e, _ := find(items, "System proxy"); e.kind != check || !e.checked || e.disabled {
		t.Fatalf("system proxy: %+v", e)
	}
	if e, _ := find(items, "Enhanced Mode"); !e.checked {
		t.Fatalf("enhanced mode: %+v", e)
	}
	if e, _ := find(items, "Rule"); e.kind != radio || !e.checked {
		t.Fatalf("rule: %+v", e)
	}
	if e, _ := find(items, "Global"); e.checked {
		t.Fatal("global checked")
	}
	if _, ok := find(items, "Quit"); ok {
		t.Fatal("the Dock brings its own Quit")
	}
	if _, ok := find(k.quickMenu(false), "Quit Kumo"); !ok {
		t.Fatal("the tray needs Quit")
	}
	// Only selectors, and GLOBAL only in global mode.
	if _, ok := find(items, "Auto —"); ok {
		t.Fatal("URLTest groups are not switched")
	}
	if _, ok := find(items, "GLOBAL —"); ok {
		t.Fatal("GLOBAL outside global mode")
	}
	g, ok := find(items, "Proxy — SG")
	if !ok {
		t.Fatal("no Proxy submenu with its resolved node")
	}
	if g.sub[0].label != "Use fastest" || g.sub[1].kind != line {
		t.Fatalf("submenu head: %+v", g.sub[:2])
	}
	if n, _ := find(g.sub, "Auto"); !n.checked || n.kind != radio {
		t.Fatalf("chosen node: %+v", n)
	}
	if n, _ := find(g.sub, "SG"); n.label != "SG    42 ms" {
		t.Fatalf("latency label: %q", n.label)
	}
	k.cfg.Mode = "global"
	if _, ok := find(k.quickMenu(true), "GLOBAL —"); !ok {
		t.Fatal("GLOBAL in global mode")
	}
	k.cfg.Mode = "direct"
	if _, ok := find(k.quickMenu(true), "Proxy —"); ok {
		t.Fatal("no node switching in direct mode")
	}
}

func TestQuickMenuOffline(t *testing.T) {
	lang = "en"
	k := menuKumo()
	k.online = false
	for _, e := range k.quickMenu(true) {
		// The diagnostics say why the core is offline.
		if e.do != nil && !e.disabled && e.label != "Run diagnostics" {
			t.Fatalf("%q is enabled while the core is offline", e.label)
		}
	}
}

func TestMenuSignature(t *testing.T) {
	k := menuKumo()
	a := menuSignature(k.quickMenu(false))
	if a != menuSignature(k.quickMenu(false)) {
		t.Fatal("unstable")
	}
	g := k.proxies["Proxy"]
	g.Now = "SG"
	k.proxies["Proxy"] = g
	if a == menuSignature(k.quickMenu(false)) {
		t.Fatal("a node switch must rebuild the menus")
	}
}

func TestExplainTunError(t *testing.T) {
	lang = "en"
	if !strings.Contains(explainTunError(tun.ErrDenied), "authorization") {
		t.Fatal("denied")
	}
	if !strings.Contains(explainTunError(errors.New("boom")), "boom") {
		t.Fatal("other")
	}
}

func TestQuickMenuStaleProxy(t *testing.T) {
	lang = "en"
	k := menuKumo()
	k.proxyOn = true
	if _, ok := find(k.quickMenu(false), "Re-apply system proxy"); ok {
		t.Fatal("re-apply shown while the proxy is in force")
	}
	k.proxyEff = proxyState{known: true, cause: sysproxy.Elsewhere, detail: "127.0.0.1:7897"}
	items := k.quickMenu(false)
	if !strings.Contains(items[0].label, "not in effect") {
		t.Fatalf("status %q", items[0].label)
	}
	if e, ok := find(items, "Re-apply system proxy"); !ok || e.disabled {
		t.Fatal("no re-apply in the tray")
	}
}
