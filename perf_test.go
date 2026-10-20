package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"kumo/clash"
	"kumo/monitor"
)

func TestPrefsMigrateFromUnversioned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"app":{"language":"system","theme":"dark"},"core":{"mixedPort":7891}}`), 0o600)
	p := loadPrefs(path)
	if p.Version != prefsVersion || p.App.Language != "" || p.App.UpdateChannel != "stable" || p.App.Theme != "dark" || p.Core.MixedPort != 7891 {
		t.Fatalf("migrated %+v", p)
	}
	var saved map[string]any
	data, _ := os.ReadFile(path)
	json.Unmarshal(data, &saved)
	if saved["version"] != float64(prefsVersion) {
		t.Fatalf("migration not saved: %s", data)
	}
	// A current file is read as it is.
	if q := loadPrefs(path); q.Version != prefsVersion || q.newer {
		t.Fatal("current file misread")
	}
}

func TestPrefsNewerNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	orig := `{"version":99,"app":{"language":"en","futureThing":true}}`
	os.WriteFile(path, []byte(orig), 0o600)
	p := loadPrefs(path)
	if !p.newer || p.App.Language != "en" {
		t.Fatalf("%+v", p)
	}
	p.App.Theme = "dark"
	if err := p.save(); err != errNewerPrefs {
		t.Fatalf("save = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != orig {
		t.Fatalf("overwritten: %s", data)
	}
}

func TestFreshPrefsAreCurrent(t *testing.T) {
	if p := loadPrefs(filepath.Join(t.TempDir(), "settings.json")); p.Version != prefsVersion {
		t.Fatal("a new file must be of the current version")
	}
}

func TestLogBatch(t *testing.T) {
	var b logBatch
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var got [][]clash.Log
	done := make(chan struct{})
	go func() {
		b.flush(ctx, 20*time.Millisecond, func(l []clash.Log) { mu.Lock(); got = append(got, l); mu.Unlock() })
		close(done)
	}()
	for i := range 500 {
		b.add(clash.Log{Payload: itoa(i)})
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || len(got[0]) != 500 {
		t.Fatalf("500 lines at once must make one batch, got %d batches", len(got))
	}
	// A flood larger than a page keeps memory flat.
	for i := range 5000 {
		b.add(clash.Log{Payload: itoa(i)})
	}
	if n := len(b.take()); n > 2*maxLogs || n < maxLogs {
		t.Fatalf("kept %d", n)
	}
}

func TestAppendLogs(t *testing.T) {
	var logs []logLine
	var seq int64
	for i := range 30 {
		logs, seq = appendLogs(logs, []clash.Log{{Payload: itoa(i)}}, 10, seq)
	}
	if len(logs) != 10 || logs[0].Payload != "20" || logs[9].seq != 30 || seq != 30 {
		t.Fatalf("%v", logs)
	}
}

func TestRedrawPages(t *testing.T) {
	for p, want := range map[string]bool{"/home": true, "/connections": true, "/statistics": true, "/logs": false, "/nodes": false, "/settings": false} {
		if showsConns(p) != want {
			t.Errorf("showsConns(%s)", p)
		}
	}
}

func TestFeedsRedrawOnlyWhatIsShown(t *testing.T) {
	k := &Kumo{prefs: &Prefs{}}
	k.shown = "/nodes"
	k.quiet(func() bool { k.memory = 1; return k.shown == "/home" })
	if k.memory != 1 {
		t.Fatal("the change was not made")
	}
}

func TestNodeRows(t *testing.T) {
	k := &Kumo{prefs: &Prefs{}, mon: monitor.New()}
	k.groups = []string{"A", "B", "GLOBAL"}
	k.proxies = map[string]clash.Proxy{
		"A":      {Name: "A", Type: "Selector", All: []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7"}},
		"B":      {Name: "B", Type: "URLTest"},
		"GLOBAL": {Name: "GLOBAL", Type: "Selector", All: []string{"A"}},
	}
	k.cfg.Mode = "rule"
	rows := k.nodeRowsOf(nil, 3)
	var kinds []string
	for _, r := range rows {
		kind := "c" + itoa(len(r.names))
		if r.names == nil {
			kind = "h"
		}
		if r.first {
			kind += "^"
		}
		if r.last {
			kind += "$"
		}
		kinds = append(kinds, r.group+kind)
	}
	if got := strings.Join(kinds, " "); got != "Ah^ Ac3 Ac3 Ac1$ Bh^$" {
		t.Fatalf("rows %s", got)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.key] {
			t.Fatalf("duplicate key %q", r.key)
		}
		seen[r.key] = true
	}
}

func TestUptime(t *testing.T) {
	m := monitor.New()
	if _, _, ok := m.Uptime("x"); ok {
		t.Fatal("untested node")
	}
	now := time.Now()
	for i := range 10 {
		ms := 50
		if i < 3 {
			ms = 0
		}
		m.Record("x", monitor.Sample{At: now, MS: ms})
	}
	up, n, ok := m.Uptime("x")
	sum, _ := m.Summary("x")
	if !ok || n != 10 || up != sum.Uptime || up != 0.7 {
		t.Fatalf("uptime %v %d, summary %v", up, n, sum.Uptime)
	}
}
