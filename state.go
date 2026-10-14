package main

import (
	"context"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kumo/clash"
	"kumo/core"
	"kumo/monitor"
	"kumo/stats"
)

// historyLen is how many one-second samples the charts keep.
const historyLen = 60

// series is a rolling window of samples for a chart.
type series []int64

func (s *series) push(v int64) {
	*s = append(*s, v)
	if len(*s) > historyLen {
		*s = (*s)[len(*s)-historyLen:]
	}
}

// Kumo is the whole state of the app. The view reads it on the main
// thread; background work changes it only through post.
type Kumo struct {
	coreBin   string // the core: bundled, else found on PATH
	tunSrc    string // the bundled core, the only one Enhanced Mode privileges
	coreSeq   atomic.Int64
	tunCtl    atomic.Pointer[clash.Client] // the bundled core's controller, for tunDown
	tunAsked  bool                         // the user was told Enhanced Mode needs authorizing
	tunBusy   bool                         // Enhanced Mode is being authorized
	dockMenu  bool                         // the Dock icon has Kumo's menu (macOS)
	menuSig   string                       // what the menus show, as last built
	speedSig  string                       // the speed the tray shows
	logLevel  string
	logPaused bool
	logFeed   func()
	logStop   context.CancelFunc

	prefs    *Prefs
	dataDir  string
	win      *mygo.Window
	tray     *mygo.Tray
	router   *ui.Router
	mgr      *core.Manager
	profiles *core.Profiles

	// The controller.
	cli     *clash.Client
	cancel  context.CancelFunc
	gen     int
	online  bool
	lastErr string
	version clash.Version
	cfg     clash.Config
	proxies map[string]clash.Proxy
	groups  []string
	rules   []clash.Rule
	conns   clash.Connections
	traffic clash.Traffic
	memory  int64
	upHist  series
	dnHist  series
	memHist series
	logs    []clash.Log
	testing map[string]bool

	// Statistics, the node monitor and node tests (features.go).
	stats      *stats.Store
	mon        *monitor.Monitor
	monCli     atomic.Pointer[clash.Client]
	monTargets atomic.Pointer[[]monitor.Target]
	freshCore  atomic.Bool // the core was (re)started since the last session
	nodeTest   nodeTestState
	detail     string // the node whose sheet is open
	detailOpen bool
	statsRange int
	statsAsk   bool
	hotkeyRec  string // the action whose shortcut is being recorded
	hotkeyErr  map[string]string

	// The bundled core.
	coreVersion string
	coreState   core.State
	coreErr     string

	// The system proxy.
	proxyOn   bool
	proxyBusy bool
	proxyErr  string
	proxyPort int
	autoDone  bool

	quitting bool
	upd      appUpdate // the in-app updater
	notice   string

	// Page state.
	sub       subForm
	connQuery string
	logQuery  string
	ruleQuery string
	connList  ui.ListState
	ruleList  ui.ListState
	logList   ui.ListState
	ctl       controllerForm
	ctlReady  bool
}

// subForm is the add-subscription form.
type subForm struct {
	url, name string
	every     string
	adding    bool
	updating  map[string]bool
}

// controllerForm edits the external controller before it is applied.
type controllerForm struct {
	host, secret, port string
	tls                bool
}

func newKumo(prefs *Prefs, dataDir string, mgr *core.Manager, profiles *core.Profiles) *Kumo {
	k := &Kumo{
		prefs:    prefs,
		dataDir:  dataDir,
		mgr:      mgr,
		profiles: profiles,
		router:   ui.NewRouter("/home"),
		proxies:  map[string]clash.Proxy{},
		testing:  map[string]bool{},
	}
	k.sub.updating = map[string]bool{}
	if mgr != nil {
		k.coreBin = mgr.Binary
		mgr.OnChange(func() {
			st, msg := mgr.Status()
			k.post(func() { k.coreState, k.coreErr = st, msg; k.refreshMenus() })
		})
	}
	return k
}

// post runs fn on the main thread and redraws the window.
func (k *Kumo) post(fn func()) {
	if k.win == nil {
		fn()
		return
	}
	k.win.Update(fn)
}

// say shows a short message at the next frame.
func (k *Kumo) say(msg string) { k.notice = msg }

// connect (re)starts the conversation with the controller.
func (k *Kumo) connect() {
	if k.cancel != nil {
		k.cancel()
	}
	host, port, secret := k.target()
	k.gen++
	k.cli = &clash.Client{Host: host, Port: port, Secret: secret, TLS: !k.bundled() && k.prefs.Controller.TLS}
	k.setTunCtl(k.cli)
	k.monCli.Store(k.cli)
	k.online = false
	ctx, cancel := context.WithCancel(context.Background())
	k.cancel = cancel
	go k.watch(ctx, k.cli, k.gen)
}

// watch waits for the controller, then keeps the state in step with it
// until ctx ends, coming back to waiting whenever it stops answering.
func (k *Kumo) watch(ctx context.Context, cli *clash.Client, gen int) {
	wait := 500 * time.Millisecond
	for ctx.Err() == nil {
		v, err := cli.Version(ctx)
		if err != nil {
			k.post(func() {
				if k.gen == gen {
					k.online, k.lastErr = false, err.Error()
				}
			})
			pause(ctx, wait)
			wait = min(wait*2, 5*time.Second)
			continue
		}
		wait = 500 * time.Millisecond
		k.post(func() {
			if k.gen != gen {
				return
			}
			k.online, k.lastErr, k.version = true, "", v
			k.refresh()
			k.onOnline()
			k.refreshMenus()
		})
		if k.stats != nil {
			k.stats.NewCore(k.freshCore.Swap(false))
		}
		session, end := context.WithCancel(ctx)
		k.streams(session, cli, gen)
		tick := time.NewTicker(15 * time.Second)
		for alive := true; alive; {
			select {
			case <-ctx.Done():
				alive = false
			case <-tick.C:
				if _, err := cli.Version(ctx); err != nil {
					alive = false
					k.post(func() {
						if k.gen == gen {
							k.online = false
							k.refreshMenus()
						}
					})
				} else {
					k.post(func() {
						if k.gen == gen {
							k.refresh()
						}
					})
				}
			}
		}
		tick.Stop()
		end()
	}
}

// streams follows the controller's live feeds.
func (k *Kumo) streams(ctx context.Context, cli *clash.Client, gen int) {
	live := func(fn func()) {
		k.post(func() {
			if k.gen == gen {
				fn()
			}
		})
	}
	follow(ctx, cli, "/traffic", nil, func(t clash.Traffic) {
		live(func() { k.traffic = t; k.upHist.push(t.Up); k.dnHist.push(t.Down) })
	})
	follow(ctx, cli, "/memory", nil, func(m clash.Memory) {
		live(func() { k.memory = m.InUse; k.memHist.push(m.InUse) })
	})
	follow(ctx, cli, "/connections", url.Values{"interval": {"1000"}}, func(c clash.Connections) {
		if k.stats != nil {
			k.stats.Record(c)
		}
		live(func() { k.conns = c })
	})
	k.logFeed = func() {
		if k.logStop != nil {
			k.logStop()
		}
		lctx, stop := context.WithCancel(ctx)
		k.logStop = stop
		follow(lctx, cli, "/logs", url.Values{"level": {k.logLevelOr()}}, func(l clash.Log) {
			live(func() {
				if k.logPaused {
					return
				}
				k.logs = append(k.logs, l)
				if len(k.logs) > 1000 {
					k.logs = k.logs[len(k.logs)-1000:]
				}
			})
		})
	}
	live(k.logFeed)
}

func (k *Kumo) logLevelOr() string {
	if k.logLevel == "" {
		return "info"
	}
	return k.logLevel
}

// setLogLevel follows the log again at level.
func (k *Kumo) setLogLevel(level string) {
	k.logLevel, k.logs = level, nil
	if k.logFeed != nil {
		k.logFeed()
	}
}

// follow reads a WebSocket feed until ctx ends, reconnecting with a
// growing pause (1 s to 30 s) that resets once a feed has lasted.
func follow[T any](ctx context.Context, cli *clash.Client, path string, q url.Values, fn func(T)) {
	go func() {
		wait := time.Second
		for ctx.Err() == nil {
			began := time.Now()
			_ = clash.Stream(ctx, cli, path, q, fn)
			if time.Since(began) > 30*time.Second {
				wait = time.Second
			}
			pause(ctx, wait)
			wait = min(wait*2, 30*time.Second)
		}
	}()
}

func pause(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// reload fetches everything again from the controller without touching
// the core: groups and nodes with their latency, the configuration and the
// rules. Connections, traffic and logs stream on their own.
func (k *Kumo) reload() {
	if !k.online {
		k.say(tr("The proxy core is still starting. Try again in a moment."))
		return
	}
	k.refresh()
	k.say(tr("Refreshed"))
}

// refresh reloads what the controller does not stream.
func (k *Kumo) refresh() {
	cli, gen := k.cli, k.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var (
			wg      sync.WaitGroup
			cfg     clash.Config
			proxies map[string]clash.Proxy
			rules   []clash.Rule
			e1, e2  error
		)
		wg.Add(3)
		go func() { defer wg.Done(); cfg, e1 = cli.Configs(ctx) }()
		go func() { defer wg.Done(); proxies, e2 = cli.Proxies(ctx) }()
		go func() { defer wg.Done(); rules, _ = cli.Rules(ctx) }()
		wg.Wait()
		k.post(func() {
			if k.gen != gen {
				return
			}
			if e1 == nil {
				k.cfg = cfg
			}
			if e2 == nil {
				k.proxies = proxies
				k.groups = groupOrder(proxies)
				k.updateTargets()
			}
			if rules != nil {
				k.rules = rules
			}
			k.refreshMenus()
		})
	}()
}

// groupOrder lists the proxy groups in the order of the profile, which
// the GLOBAL group keeps, GLOBAL itself last.
func groupOrder(proxies map[string]clash.Proxy) []string {
	var out []string
	if g, ok := proxies["GLOBAL"]; ok {
		for _, n := range g.All {
			if p, ok := proxies[n]; ok && p.IsGroup() && !p.Hidden {
				out = append(out, n)
			}
		}
	}
	for n, p := range proxies {
		if p.IsGroup() && n != "GLOBAL" && !slices.Contains(out, n) && !p.Hidden {
			out = append(out, n)
		}
	}
	if _, ok := proxies["GLOBAL"]; ok {
		out = append(out, "GLOBAL")
	}
	return out
}

// call runs a controller request in the background, reports its error,
// then refreshes.
func (k *Kumo) call(fn func(ctx context.Context, cli *clash.Client) error, done func()) {
	cli := k.cli
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := fn(ctx, cli)
		cancel()
		k.post(func() {
			if err != nil {
				k.say(tr("Request failed: ") + err.Error())
			} else if done != nil {
				done()
			}
			k.refresh()
		})
	}()
}

func (k *Kumo) setMode(mode string) {
	k.cfg.Mode = mode
	k.call(func(ctx context.Context, cli *clash.Client) error {
		return cli.PatchConfigs(ctx, map[string]any{"mode": mode})
	}, nil)
	k.refreshMenus()
}

func (k *Kumo) patch(fields map[string]any) {
	k.call(func(ctx context.Context, cli *clash.Client) error { return cli.PatchConfigs(ctx, fields) }, nil)
}

// choose selects node in group.
func (k *Kumo) choose(group, node string) {
	if p, ok := k.proxies[group]; ok {
		p.Now = node
		k.proxies[group] = p
	}
	k.call(func(ctx context.Context, cli *clash.Client) error { return cli.Select(ctx, group, node) }, nil)
	k.refreshMenus()
}

// measure tests the latency of nodes, a few at a time, then runs done.
func (k *Kumo) measure(nodes []string, done func()) {
	cli := k.cli
	testURL := k.prefs.Controller.TestURL
	timeout := time.Duration(max(k.prefs.Controller.TestTimeout, 1000)) * time.Millisecond
	var todo []string
	for _, n := range nodes {
		if p, ok := k.proxies[n]; ok && !p.IsGroup() && !k.testing[n] {
			k.testing[n] = true
			todo = append(todo, n)
		}
	}
	if len(todo) == 0 {
		if done != nil {
			done()
		}
		return
	}
	go func() {
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for _, n := range todo {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), timeout+2*time.Second)
				_, _ = cli.TestDelay(ctx, n, testURL, timeout)
				cancel()
				<-sem
				k.post(func() { delete(k.testing, n) })
			}()
		}
		wg.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		proxies, err := cli.Proxies(ctx)
		cancel()
		k.post(func() {
			if err == nil {
				k.proxies = proxies
				k.groups = groupOrder(proxies)
			}
			if done != nil {
				done()
			}
		})
	}()
}

// leaves are the nodes of a group, without nested groups.
func (k *Kumo) leaves(group string) []string {
	var out []string
	for _, n := range k.proxies[group].All {
		if p, ok := k.proxies[n]; ok && !p.IsGroup() {
			out = append(out, n)
		}
	}
	return out
}

// fastest is the node of group with the lowest measured latency.
func (k *Kumo) fastest(group string) (string, int) {
	best, bestMS := "", 0
	for _, n := range k.leaves(group) {
		if ms := k.proxies[n].LastDelay(); ms > 0 && (bestMS == 0 || ms < bestMS) {
			best, bestMS = n, ms
		}
	}
	return best, bestMS
}

// pickFastest measures group and selects its fastest node.
func (k *Kumo) pickFastest(group string) {
	k.measure(k.leaves(group), func() {
		if n, ms := k.fastest(group); n != "" {
			k.choose(group, n)
			k.say(tr("Switched to the fastest node: ") + n + " · " + itoa(ms) + " ms")
		} else {
			k.say(tr("No node answered the latency test."))
		}
	})
}

// primaryGroup is the group the home screen switches: GLOBAL in global
// mode, otherwise the profile's first selector.
func (k *Kumo) primaryGroup() (clash.Proxy, bool) {
	if k.cfg.Mode == "global" {
		g, ok := k.proxies["GLOBAL"]
		return g, ok
	}
	for _, n := range k.groups {
		if g := k.proxies[n]; g.Type == "Selector" && n != "GLOBAL" {
			return g, true
		}
	}
	return clash.Proxy{}, false
}

// resolve follows a group's selections down to a node.
func (k *Kumo) resolve(name string) string {
	for range 10 {
		p, ok := k.proxies[name]
		if !ok || !p.IsGroup() || p.Now == "" {
			return name
		}
		name = p.Now
	}
	return name
}

// activeNode is the node traffic leaves through, as the home screen says.
func (k *Kumo) activeNode() string {
	if k.cfg.Mode == "direct" {
		return "DIRECT"
	}
	if g, ok := k.primaryGroup(); ok {
		return k.resolve(g.Name)
	}
	return "—"
}
