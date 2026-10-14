package main

import (
	"testing"

	"kumo/clash"
	"kumo/core"
	"kumo/monitor"
)

func TestUpdateTargets(t *testing.T) {
	k := newKumo(loadPrefs(t.TempDir()+"/s.json"), t.TempDir(), nil, nil)
	k.mon = monitor.New()
	k.proxies = map[string]clash.Proxy{
		"Proxy":        {Name: "Proxy", Type: "Selector", All: []string{"Auto", "A", "B", "DIRECT", core.TestGroup}},
		"Auto":         {Name: "Auto", Type: "URLTest", All: []string{"A", "C"}, TestURL: "https://auto.test/204"},
		core.TestGroup: {Name: core.TestGroup, Type: "Selector", All: []string{"A", "B", "C"}, Hidden: true},
		"A":            {Name: "A", Type: "Shadowsocks"}, "B": {Name: "B", Type: "Trojan"}, "C": {Name: "C", Type: "Vmess"},
		"DIRECT": {Name: "DIRECT", Type: "Direct"},
	}
	k.groups = groupOrder(k.proxies)
	k.mon.Record("gone", monitor.Sample{MS: 1})
	k.updateTargets()
	got := map[string]string{}
	for _, tg := range *k.monTargets.Load() {
		got[tg.Name] = tg.URL
	}
	def := k.prefs.Controller.TestURL
	if len(got) != 3 || got["A"] != "https://auto.test/204" || got["C"] != "https://auto.test/204" || got["B"] != def {
		t.Fatalf("%v", got)
	}
	if _, ok := k.mon.Summary("gone"); ok {
		t.Fatal("a node no longer listed was kept")
	}
}
