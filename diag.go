package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"kumo/clash"
	"kumo/core"
	"kumo/sysproxy"
)

// One-click diagnostics: checks in order, from the core to a request
// through Kumo, each shown as it finishes with what it found in plain
// words and, where Kumo can, the fix.

type diagStatus int

const (
	diagPending diagStatus = iota
	diagRunning
	diagOK
	diagWarn
	diagFail
	diagSkip
)

func (s diagStatus) mark() string {
	return [...]string{"·", "…", "✓", "⚠", "✗", "–"}[s]
}

type fixKind int

const (
	fixNone fixKind = iota
	fixReapply
	fixProxyOn
	fixRestart
	fixUpdateSub
	fixFastest
)

// diagStep is one check and what it found.
type diagStep struct {
	key    string
	title  string
	status diagStatus
	detail string
	fix    fixKind
	arg    string // the subscription or group the fix is for
}

// diagState is the diagnostics sheet.
type diagState struct {
	open    bool
	running bool
	run     int // the run the steps belong to
	steps   []diagStep
	at      time.Time
}

// diagEnv is what the checks need, read on the main thread before they
// run in the background.
type diagEnv struct {
	cli         *clash.Client
	online      bool
	bundled     bool
	mgr         *core.Manager
	runtime     string
	coreState   core.State
	coreErr     string
	profile     core.Profile
	hasProfile  bool
	proxyOn     bool
	port        int
	controller  string
	tunOn       bool
	tunWanted   bool
	tunSrc      string
	mode        string
	groups      [][2]string // group, the node it uses
	testURL     string
	timeout     time.Duration
	now         time.Time
	readProxy   func() (sysproxy.Effective, error)
	directProbe func(ctx context.Context) (int, string, error)
	viaKumo     func(ctx context.Context, port int, target string) (int, error)
	dial        func(addr string) bool
}

// diagSteps are the checks in the order they run.
func diagSteps() []diagStep {
	return []diagStep{
		{key: "core", title: tr("Core running and controller reachable")},
		{key: "config", title: tr("Configuration valid")},
		{key: "sub", title: tr("Subscription present and recently updated")},
		{key: "proxy", title: tr("System proxy in effect")},
		{key: "tun", title: tr("Enhanced Mode")},
		{key: "dns", title: tr("DNS through the core")},
		{key: "direct", title: tr("Internet reachable directly")},
		{key: "nodes", title: tr("Latency of the nodes in use")},
		{key: "via", title: tr("Test request through Kumo")},
		{key: "portal", title: tr("Captive portal (hotel or airport Wi-Fi login)")},
		{key: "ports", title: tr("Port conflicts")},
	}
}

func (k *Kumo) openDiagnostics() {
	k.diag.open = true
	k.show()
	if !k.diag.running {
		k.runDiagnostics()
	}
}

// diagEnvNow reads what the checks need, on the main thread.
func (k *Kumo) diagEnvNow() diagEnv {
	e := diagEnv{
		cli: k.cli, online: k.online, bundled: k.bundled(), mgr: k.mgr, coreState: k.coreState, coreErr: k.coreErr,
		proxyOn: k.proxyOn, port: k.listenPort(), tunOn: k.tunOn(), tunWanted: k.prefs.Core.Tun, tunSrc: k.tunSrc,
		mode: k.cfg.Mode, testURL: k.prefs.Controller.TestURL,
		timeout: time.Duration(max(k.prefs.Controller.TestTimeout, 1000)) * time.Millisecond, now: time.Now(),
		readProxy: readProxy, directProbe: directProbe, viaKumo: viaKumo, dial: dialOK,
	}
	if e.testURL == "" {
		e.testURL = "https://www.gstatic.com/generate_204"
	}
	if k.mgr != nil {
		e.runtime = k.runtimeFile()
	}
	if k.cli != nil {
		e.controller = net.JoinHostPort(k.cli.Host, strconv.Itoa(k.cli.Port))
	}
	e.profile, e.hasProfile = k.profiles.Find(k.profiles.Active())
	if k.cfg.Mode != "direct" {
		for _, g := range k.menuGroups() {
			e.groups = append(e.groups, [2]string{g, k.resolve(g)})
			if len(e.groups) == 4 {
				break
			}
		}
	}
	return e
}

// runDiagnostics runs the checks one after another, showing each.
func (k *Kumo) runDiagnostics() {
	k.diag.run++
	run := k.diag.run
	k.diag.running, k.diag.steps, k.diag.at = true, diagSteps(), time.Now()
	env := k.diagEnvNow()
	go func() {
		results := map[string]diagStep{}
		for i, st := range diagSteps() {
			k.post(func() {
				if k.diag.run == run {
					k.diag.steps[i].status = diagRunning
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			got := runCheck(ctx, st, env, results)
			cancel()
			results[st.key] = got
			k.post(func() {
				if k.diag.run == run {
					k.diag.steps[i] = got
				}
			})
		}
		k.post(func() {
			if k.diag.run == run {
				k.diag.running = false
			}
		})
	}()
}

// runCheck runs one check; done holds the steps before it.
func runCheck(ctx context.Context, st diagStep, e diagEnv, done map[string]diagStep) diagStep {
	ok := func(d string) diagStep { st.status, st.detail = diagOK, d; return st }
	warn := func(d string) diagStep { st.status, st.detail = diagWarn, d; return st }
	fail := func(d string) diagStep { st.status, st.detail = diagFail, d; return st }
	skip := func(d string) diagStep { st.status, st.detail = diagSkip, d; return st }
	coreUp := done["core"].status == diagOK
	switch st.key {
	case "core":
		if e.bundled && e.coreState == core.Failed {
			st.fix = fixRestart
			return fail(tr("The core stopped: ") + explainCoreError(e.coreErr))
		}
		if e.cli == nil {
			return fail(tr("No controller is set up."))
		}
		v, err := e.cli.Version(ctx)
		if err != nil {
			if e.bundled {
				st.fix = fixRestart
			}
			return fail(fmt.Sprintf(tr("The controller at %s does not answer: %v"), e.controller, err))
		}
		return ok(fmt.Sprintf(tr("mihomo %s answers at %s."), v.Version, e.controller))
	case "config":
		if !coreUp {
			return skip(tr("Needs the core."))
		}
		if e.bundled && e.mgr != nil && e.runtime != "" {
			if err := e.mgr.Test(e.runtime); err != nil {
				st.fix = fixRestart
				return fail(tr("The core rejects the configuration: ") + err.Error())
			}
			return ok(tr("The core accepts the configuration in use."))
		}
		if _, err := e.cli.Configs(ctx); err != nil {
			return fail(err.Error())
		}
		return ok(tr("The external core runs its configuration."))
	case "sub":
		if !e.bundled {
			return skip(tr("An external core uses its own configuration."))
		}
		if !e.hasProfile {
			return warn(tr("No subscription: Kumo runs direct, with no node. Add one on the Subscriptions page."))
		}
		if !e.profile.Remote() {
			return ok(fmt.Sprintf(tr("“%s”, a local file."), e.profile.Name))
		}
		age := e.now.Sub(e.profile.UpdatedAt)
		limit := 7 * 24 * time.Hour
		if h := e.profile.AutoUpdateHours; h > 0 {
			limit = max(2*time.Duration(h)*time.Hour, 48*time.Hour)
		}
		if e.profile.Expire > 0 && time.Unix(e.profile.Expire, 0).Before(e.now) {
			st.fix, st.arg = fixUpdateSub, e.profile.ID
			return fail(fmt.Sprintf(tr("“%s” expired on %s; renew it with your provider."), e.profile.Name, time.Unix(e.profile.Expire, 0).Format("2006-01-02")))
		}
		if age > limit {
			st.fix, st.arg = fixUpdateSub, e.profile.ID
			return warn(fmt.Sprintf(tr("“%s” was last updated %s ago; its nodes may have changed."), e.profile.Name, ago(age)))
		}
		return ok(fmt.Sprintf(tr("“%s”, updated %s ago."), e.profile.Name, ago(age)))
	case "proxy":
		if !e.proxyOn {
			if e.tunOn {
				return skip(tr("Off; Enhanced Mode routes apps instead."))
			}
			st.fix = fixProxyOn
			return warn(tr("The system proxy is off: only apps set up to use Kumo go through it."))
		}
		eff, err := e.readProxy()
		if err != nil {
			return skip(tr("The system's proxy settings cannot be read here."))
		}
		cause, detail := eff.Check(e.port)
		if cause == sysproxy.Applied {
			return ok(fmt.Sprintf(tr("Apps that follow the system proxy go to 127.0.0.1:%d (%s)."), e.port, eff.Source))
		}
		st.fix = fixReapply
		return warn(proxyCause(proxyState{known: true, cause: cause, detail: detail}))
	case "tun":
		if e.tunOn {
			return ok(tr("On: all traffic goes through Kumo's TUN device."))
		}
		if e.tunWanted {
			if _, authorized := tunCore(e.tunSrc); !authorized && e.tunSrc != "" {
				return warn(tr("Turned on but not authorized: turn it on again to authorize it."))
			}
			return warn(tr("Turned on, but the core runs without it."))
		}
		return skip(tr("Off. Turn it on for apps that ignore the system proxy."))
	case "dns":
		if !coreUp {
			return skip(tr("Needs the core."))
		}
		ips, err := e.cli.DNSQuery(ctx, "www.gstatic.com", "A")
		if err != nil || len(ips) == 0 {
			if err == nil {
				err = errors.New(tr("no answer"))
			}
			return fail(tr("The core could not resolve www.gstatic.com: ") + err.Error())
		}
		return ok("www.gstatic.com → " + strings.Join(ips[:min(len(ips), 2)], ", "))
	case "direct":
		code, loc, err := e.directProbe(ctx)
		switch {
		case err != nil:
			if e.dial("1.1.1.1:443") {
				return warn(tr("Web requests fail without Kumo, though the Internet answers: a firewall may block them. ") + err.Error())
			}
			return fail(tr("No Internet without Kumo: check Wi-Fi or the cable. ") + err.Error())
		case code == http.StatusNoContent:
			return ok(tr("This computer reaches the Internet without Kumo."))
		default:
			_ = loc
			return warn(fmt.Sprintf(tr("An unexpected answer (HTTP %d): a login page may be in the way."), code))
		}
	case "nodes":
		if !coreUp {
			return skip(tr("Needs the core."))
		}
		if e.mode == "direct" || len(e.groups) == 0 {
			return skip(tr("No node in direct mode"))
		}
		var parts []string
		worst := diagOK
		for _, g := range e.groups {
			ms, err := e.cli.TestDelay(ctx, g[1], e.testURL, e.timeout)
			switch {
			case err != nil || ms <= 0:
				parts = append(parts, fmt.Sprintf("%s › %s: %s", g[0], g[1], tr("Timeout")))
				worst = diagFail
				if st.arg == "" {
					st.fix, st.arg = fixFastest, g[0]
				}
			case ms > 1000:
				parts = append(parts, fmt.Sprintf("%s › %s: %d ms", g[0], g[1], ms))
				worst = max(worst, diagWarn)
				if st.arg == "" {
					st.fix, st.arg = fixFastest, g[0]
				}
			default:
				parts = append(parts, fmt.Sprintf("%s › %s: %d ms", g[0], g[1], ms))
			}
		}
		st.status, st.detail = worst, strings.Join(parts, "\n")
		return st
	case "via":
		if !coreUp {
			return skip(tr("Needs the core."))
		}
		code, err := e.viaKumo(ctx, e.port, e.testURL)
		switch {
		case err != nil:
			if done["nodes"].status == diagFail {
				st.fix, st.arg = done["nodes"].fix, done["nodes"].arg
			}
			return fail(fmt.Sprintf(tr("A request through 127.0.0.1:%d failed: %v"), e.port, err))
		case code/100 == 2:
			return ok(fmt.Sprintf(tr("%s answered through Kumo (HTTP %d)."), hostOf(e.testURL), code))
		}
		return warn(fmt.Sprintf(tr("%s answered through Kumo with HTTP %d."), hostOf(e.testURL), code))
	case "portal":
		d := done["direct"]
		switch {
		case d.status == diagOK:
			return ok(tr("None: the network let a check page through untouched."))
		case d.status == diagWarn && strings.Contains(d.detail, "HTTP"):
			return warn(tr("Likely: the network answers with its own page. Open a browser and sign in to the Wi-Fi, then run this again."))
		}
		return skip(tr("Cannot tell without the Internet."))
	case "ports":
		if e.bundled && e.coreState == core.Failed && strings.Contains(e.coreErr, "address already in use") {
			st.fix = fixRestart
			return fail(tr("Another app holds a port the core needs: ") + e.coreErr)
		}
		if !coreUp {
			return skip(tr("Needs the core."))
		}
		if !e.dial(net.JoinHostPort("127.0.0.1", strconv.Itoa(e.port))) {
			st.fix = fixRestart
			return fail(fmt.Sprintf(tr("Nothing listens on 127.0.0.1:%d, the port the system proxy points at."), e.port))
		}
		return ok(fmt.Sprintf(tr("The core listens on %d, its controller on %s."), e.port, e.controller))
	}
	return skip("")
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// directProbe asks a check page for its empty answer, around any proxy.
// A captive portal answers it with its login page, or a redirect to it.
func directProbe(ctx context.Context) (int, string, error) {
	cl := &http.Client{
		Transport:     &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       8 * time.Second,
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://connectivitycheck.gstatic.com/generate_204", nil)
	res, err := cl.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, res.Header.Get("Location"), nil
}

// viaKumo requests target through Kumo's HTTP proxy at port.
func viaKumo(ctx context.Context, port int, target string) (int, error) {
	proxy := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
	cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}, Timeout: 12 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Kumo/"+version+" diagnostics")
	res, err := cl.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, nil
}

func dialOK(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// diagFix runs a step's fix, then checks again.
func (k *Kumo) diagFix(st diagStep) {
	switch st.fix {
	case fixReapply:
		k.reapplyProxy()
	case fixProxyOn:
		k.setSystemProxy(true)
	case fixRestart:
		k.startCore()
	case fixUpdateSub:
		k.updateProfile(st.arg)
	case fixFastest:
		k.pickFastest(st.arg)
	}
}

func fixLabel(f fixKind) string {
	switch f {
	case fixReapply:
		return tr("Re-apply")
	case fixProxyOn:
		return tr("Turn on")
	case fixRestart:
		return tr("Restart core")
	case fixUpdateSub:
		return tr("Update subscription")
	case fixFastest:
		return tr("Use fastest")
	}
	return ""
}

// diagText is the results as plain text, for support.
func (k *Kumo) diagText() string {
	var b strings.Builder
	_, off := k.diag.at.Zone()
	fmt.Fprintf(&b, "Kumo %s · %s/%s · %s (UTC%+d)\n", version, runtime.GOOS, runtime.GOARCH, k.diag.at.Format("2006-01-02 15:04:05"), off/3600)
	if k.version.Version != "" {
		fmt.Fprintf(&b, "mihomo %s · %s %s\n", k.version.Version, tr("Mode"), modeLabel(k.cfg.Mode))
	}
	for _, st := range k.diag.steps {
		fmt.Fprintf(&b, "%s %s", st.status.mark(), st.title)
		if st.detail != "" {
			fmt.Fprintf(&b, ": %s", strings.ReplaceAll(st.detail, "\n", "; "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

