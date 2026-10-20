package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"kumo/clash"
	"kumo/core"
	"kumo/sysproxy"
)

func TestRouteExplain(t *testing.T) {
	lang = "en"
	cn := clash.Connection{Rule: "DomainSuffix", Payload: "google.com", Chains: []string{"HK-01", "Proxy"}}
	r := routeOf(cn)
	if r.matched() != "DOMAIN-SUFFIX,google.com" || r.node() != "HK-01" {
		t.Fatalf("%+v", r)
	}
	if got := r.explain("rule"); got != "Matched DOMAIN-SUFFIX,google.com → group Proxy → node HK-01" {
		t.Fatalf("explain %q", got)
	}
	d := routeOf(clash.Connection{Rule: "GeoIP", Payload: "CN", Chains: []string{"DIRECT"}})
	if got := d.explain("rule"); got != "Matched GEOIP,CN → out directly, without a node" {
		t.Fatalf("direct %q", got)
	}
	m := routeOf(clash.Connection{Rule: "Match", Chains: []string{"JP-02", "Auto", "Proxy"}})
	if got := m.explain("rule"); !strings.HasPrefix(got, "No other rule matched") || !strings.HasSuffix(got, "group Proxy → group Auto → node JP-02") {
		t.Fatalf("match %q", got)
	}
	lang = "zh"
	if got := routeOf(cn).explain("rule"); !strings.Contains(got, "DOMAIN-SUFFIX,google.com") || strings.Contains(got, "Matched") {
		t.Fatalf("zh %q", got)
	}
	lang = "en"
}

func TestRouteFromLog(t *testing.T) {
	r, ok := routeFromLog("[TCP] 127.0.0.1:54321 --> www.google.com:443 match DomainSuffix(google.com) using Proxy[Auto[HK-01]]", 54321)
	if !ok || r.matched() != "DOMAIN-SUFFIX,google.com" || strings.Join(r.chain, ">") != "Proxy>Auto>HK-01" {
		t.Fatalf("%+v %v", r, ok)
	}
	if _, ok := routeFromLog("[TCP] 127.0.0.1:54321 --> www.google.com:443 match DomainSuffix(google.com) using Proxy[HK-01]", 1); ok {
		t.Fatal("another port matched")
	}
	r, ok = routeFromLog("[TCP] dial Proxy (match DomainSuffix/google.com) 127.0.0.1:5000(curl, uid=501) --> www.google.com:443 error: dial timeout", 5000)
	if !ok || r.matched() != "DOMAIN-SUFFIX,google.com" || r.node() != "Proxy" {
		t.Fatalf("dial error: %+v %v", r, ok)
	}
	r, ok = routeFromLog("[TCP] 127.0.0.1:51462(kumo, uid=1000) --> h9.test:80 match Match using PROXY[N247]", 51462)
	if !ok || r.matched() != "MATCH" || strings.Join(r.chain, ">") != "PROXY>N247" {
		t.Fatalf("with process: %+v %v", r, ok)
	}
	r, ok = routeFromLog("[TCP] 127.0.0.1:7000 --> a.b:443 match GeoSite(google) using Proxy[🇭🇰 HK 01]", 7000)
	if !ok || r.node() != "🇭🇰 HK 01" {
		t.Fatalf("spaces: %+v", r)
	}
	r, ok = routeFromLog("[TCP] 127.0.0.1:6000 --> example.org:80 doesn't match any rule using DIRECT", 6000)
	if !ok || r.matched() != "MATCH" || r.node() != "DIRECT" {
		t.Fatalf("no rule: %+v %v", r, ok)
	}
}

func TestTestURL(t *testing.T) {
	for in, want := range map[string]string{"example.com": "https://example.com/", "http://a.b/c?d": "http://a.b/c?d", " github.com:8443 ": "https://github.com:8443/"} {
		if got, err := testURL(in); err != nil || got != want {
			t.Errorf("%q → %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "ftp://x", "https://"} {
		if _, err := testURL(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func diagTestEnv() diagEnv {
	return diagEnv{
		bundled: true, coreState: core.Running, proxyOn: true, port: 7890, mode: "rule", now: time.Now(),
		hasProfile: true, profile: core.Profile{Name: "Demo", URL: "https://x", UpdatedAt: time.Now().Add(-time.Hour), AutoUpdateHours: 24},
		readProxy: func() (sysproxy.Effective, error) {
			return sysproxy.Effective{HTTP: "127.0.0.1:7890", HTTPS: "127.0.0.1:7890", Source: "test"}, nil
		},
		directProbe: func(context.Context) (int, string, error) { return http.StatusNoContent, "", nil },
		dial:        func(string) bool { return true },
	}
}

func TestDiagChecks(t *testing.T) {
	lang = "en"
	ctx := context.Background()
	done := map[string]diagStep{"core": {status: diagOK}}
	step := func(key string, e diagEnv) diagStep {
		for _, st := range diagSteps() {
			if st.key == key {
				return runCheck(ctx, st, e, done)
			}
		}
		t.Fatalf("no step %s", key)
		return diagStep{}
	}
	e := diagTestEnv()
	if st := step("proxy", e); st.status != diagOK {
		t.Fatalf("proxy %+v", st)
	}
	e.readProxy = func() (sysproxy.Effective, error) {
		return sysproxy.Effective{HTTP: "127.0.0.1:7897", HTTPS: "127.0.0.1:7897"}, nil
	}
	if st := step("proxy", e); st.status != diagWarn || st.fix != fixReapply || !strings.Contains(st.detail, "127.0.0.1:7897") {
		t.Fatalf("taken over %+v", st)
	}
	e.proxyOn = false
	if st := step("proxy", e); st.status != diagWarn || st.fix != fixProxyOn {
		t.Fatalf("off %+v", st)
	}
	e = diagTestEnv()
	e.profile.UpdatedAt = time.Now().Add(-10 * 24 * time.Hour)
	if st := step("sub", e); st.status != diagWarn || st.fix != fixUpdateSub {
		t.Fatalf("stale sub %+v", st)
	}
	e.hasProfile = false
	if st := step("sub", e); st.status != diagWarn {
		t.Fatalf("no sub %+v", st)
	}
	e = diagTestEnv()
	e.directProbe = func(context.Context) (int, string, error) { return 302, "http://login.hotel/", nil }
	d := step("direct", e)
	if d.status != diagWarn {
		t.Fatalf("portal direct %+v", d)
	}
	done["direct"] = d
	if st := step("portal", e); st.status != diagWarn {
		t.Fatalf("portal %+v", st)
	}
	e.directProbe = func(context.Context) (int, string, error) { return 0, "", errors.New("no route to host") }
	e.dial = func(string) bool { return false }
	if st := step("direct", e); st.status != diagFail {
		t.Fatalf("offline %+v", st)
	}
	e = diagTestEnv()
	e.coreState, e.coreErr = core.Failed, "listen tcp 127.0.0.1:7890: bind: address already in use"
	if st := step("ports", e); st.status != diagFail || st.fix != fixRestart {
		t.Fatalf("ports %+v", st)
	}
	if st := step("core", e); st.status != diagFail || st.fix != fixRestart {
		t.Fatalf("core %+v", st)
	}
	// What needs the core is skipped without it.
	done["core"] = diagStep{status: diagFail}
	for _, key := range []string{"config", "dns", "nodes", "via"} {
		if st := step(key, diagTestEnv()); st.status != diagSkip {
			t.Fatalf("%s ran without the core: %+v", key, st)
		}
	}
}

func TestDiagText(t *testing.T) {
	lang = "en"
	k := newKumo(&Prefs{}, "", nil, nil)
	k.diag.at = time.Date(2026, 10, 20, 13, 0, 0, 0, time.FixedZone("", 8*3600))
	k.diag.steps = diagSteps()
	k.diag.steps[0].status, k.diag.steps[0].detail = diagOK, "mihomo v1 answers"
	k.diag.steps[3].status, k.diag.steps[3].detail = diagWarn, "line one\nline two"
	txt := k.diagText()
	if !strings.Contains(txt, "(UTC+8)") || !strings.Contains(txt, "✓ Core running and controller reachable: mihomo v1 answers") ||
		!strings.Contains(txt, "⚠ System proxy in effect: line one; line two") {
		t.Fatal(txt)
	}
}
