package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"kumo/clash"
	"kumo/core"
	"kumo/monitor"
	"kumo/nodetest"
	"kumo/stats"
)

// Traffic statistics, the node monitor, node tests and global hotkeys.

// monEvery and monTimeout are the monitor's settings, read off the main
// thread.
var monEvery, monTimeout atomic.Int64

// startFeatures opens the statistics and starts the monitor. It runs once,
// on the main thread, before the core starts.
func (k *Kumo) startFeatures() {
	st, err := stats.Open(filepath.Join(k.dataDir, "traffic-stats.json.gz"))
	if err != nil {
		k.say(tr("The traffic statistics could not be read and start afresh: ") + err.Error())
	}
	k.stats = st
	k.mon = monitor.New()
	k.syncMonitorPrefs()
	go func() {
		for range time.Tick(time.Minute) {
			_ = st.Save()
		}
	}()
	go k.mon.Run(context.Background(), func() time.Duration { return time.Duration(monEvery.Load()) * time.Second }, 3,
		func() []monitor.Target {
			if t := k.monTargets.Load(); t != nil {
				return *t
			}
			return nil
		}, k.probe)
	k.registerHotkeys()
}

func (k *Kumo) syncMonitorPrefs() {
	monEvery.Store(int64(max(k.prefs.App.MonitorEvery, 0)))
	monTimeout.Store(int64(max(k.prefs.Controller.TestTimeout, 1000)))
}

// probe is the monitor's latency test, through the core's controller. A
// controller that does not answer skips the test rather than failing the
// node.
func (k *Kumo) probe(ctx context.Context, t monitor.Target) (int, error) {
	cli := k.monCli.Load()
	if cli == nil {
		return 0, monitor.ErrSkip
	}
	timeout := time.Duration(monTimeout.Load()) * time.Millisecond
	tctx, cancel := context.WithTimeout(ctx, timeout+3*time.Second)
	defer cancel()
	ms, err := cli.TestDelay(tctx, t.Name, t.URL, timeout)
	var st *clash.StatusError
	if err != nil && !errors.As(err, &st) {
		return 0, monitor.ErrSkip
	}
	return ms, err
}

// testable says whether the monitor and the node tests take p.
func testable(p clash.Proxy) bool {
	switch p.Type {
	case "Direct", "Reject", "RejectDrop", "Pass", "Compatible", "Dns":
		return false
	}
	return !p.IsGroup()
}

// updateTargets gives the monitor the nodes of the groups shown, each with
// the test URL of the first group that sets one for it, else Kumo's.
func (k *Kumo) updateTargets() {
	url := map[string]string{}
	var order []string
	for _, g := range k.groups {
		gp := k.proxies[g]
		for _, n := range gp.All {
			p, ok := k.proxies[n]
			if !ok || !testable(p) {
				continue
			}
			if _, seen := url[n]; !seen {
				order = append(order, n)
				url[n] = ""
			}
			if url[n] == "" {
				url[n] = gp.TestURL
			}
		}
	}
	list := make([]monitor.Target, 0, len(order))
	for _, n := range order {
		u := url[n]
		if u == "" {
			u = k.prefs.Controller.TestURL
		}
		list = append(list, monitor.Target{Name: n, URL: u})
	}
	k.monTargets.Store(&list)
	k.mon.Forget(func(n string) bool { _, ok := url[n]; return ok })
}

// nodeTestState is the node test under way, one at a time: they share the
// test listener.
type nodeTestState struct {
	name, kind string // kind: "speed" or "udp"
	cancel     context.CancelFunc
	progress   nodetest.Speed
}

// Speed test limits.
const (
	speedBytes = 50 << 20
	speedTime  = 8 * time.Second
)

// testNode runs a speed ("speed") or UDP ("udp") test of one node through
// the test listener, whose hidden group is set to the node; the user's own
// groups are not touched.
func (k *Kumo) testNode(name, kind string) {
	switch {
	case !k.bundled() || k.prefs.Core.TestPort == 0:
		k.say(tr("Speed and UDP tests need the bundled core."))
		return
	case !k.online:
		k.say(tr("The proxy core is still starting. Try again in a moment."))
		return
	case k.nodeTest.name != "":
		k.say(tr("Another node test is running."))
		return
	}
	if kind == "speed" && !k.prefs.App.SpeedWarned {
		go func() {
			res, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: k.win, Type: mygo.MessageInfo,
				Message: tr("Test download speed?"),
				Detail:  fmt.Sprintf(tr("Kumo downloads from Cloudflare's speed test through %s for up to %d seconds, at most %d MB. On a metered plan this uses data."), name, int(speedTime.Seconds()), speedBytes>>20),
				Buttons: []string{tr("Test"), tr("Cancel")}, DefaultButton: 0, CancelButton: 1})
			k.post(func() {
				if err == nil && res.Button == 0 {
					k.prefs.App.SpeedWarned = true
					_ = k.prefs.save()
					k.testNode(name, kind)
				}
			})
		}()
		return
	}
	cli, port, mon := k.cli, k.prefs.Core.TestPort, k.mon
	ctx, cancel := context.WithCancel(context.Background())
	k.nodeTest = nodeTestState{name: name, kind: kind, cancel: cancel}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	go func() {
		defer cancel()
		finish := func(msg string) {
			k.post(func() {
				k.nodeTest = nodeTestState{}
				if msg != "" {
					k.say(msg)
				}
			})
		}
		sctx, scancel := context.WithTimeout(ctx, 5*time.Second)
		err := cli.Select(sctx, core.TestGroup, name)
		scancel()
		if err != nil {
			finish(tr("The node test could not start: ") + err.Error())
			return
		}
		switch kind {
		case "speed":
			sp, err := nodetest.Download(ctx, addr, nodetest.SpeedURL+fmt.Sprint(speedBytes), speedBytes, speedTime, func(s nodetest.Speed) {
				k.post(func() { k.nodeTest.progress = s })
			})
			if ctx.Err() != nil {
				finish(tr("Speed test canceled."))
				return
			}
			mon.SetSpeed(name, sp.Mbps(), time.Now(), err)
			if err != nil {
				finish(fmt.Sprintf(tr("%s: the speed test failed: %v"), name, err))
				return
			}
			finish(fmt.Sprintf(tr("%s: %s Mbps"), name, mbps(sp.Mbps())))
		case "udp":
			r, err := nodetest.CheckUDP(ctx, addr, "1.1.1.1:53", "cloudflare.com", 6*time.Second)
			if ctx.Err() != nil {
				finish("")
				return
			}
			mon.SetUDP(name, int(r), time.Now())
			msg := fmt.Sprintf(tr("%s: %s"), name, udpText(int(r)))
			if r == nodetest.UDPUnknown && err != nil {
				msg += " (" + err.Error() + ")"
			}
			finish(msg)
		}
	}()
}

// cancelNodeTest stops the node test under way.
func (k *Kumo) cancelNodeTest() {
	if k.nodeTest.cancel != nil {
		k.nodeTest.cancel()
	}
}

func mbps(v float64) string {
	switch {
	case v >= 100:
		return fmt.Sprintf("%.0f", v)
	case v >= 10:
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

func udpText(r int) string {
	switch r {
	case int(nodetest.UDPSupported):
		return tr("UDP supported")
	case int(nodetest.UDPUnsupported):
		return tr("UDP not supported")
	}
	return tr("UDP unknown")
}

// Global hotkeys.

// hotkeyAction is something a global shortcut does.
type hotkeyAction struct {
	id, title string
	run       func(k *Kumo)
}

func hotkeyActions() []hotkeyAction {
	return []hotkeyAction{
		{"proxy", tr("Turn the system proxy on or off"), func(k *Kumo) {
			if !k.proxyBusy {
				k.setSystemProxy(!k.proxyOn)
			}
		}},
		{"tun", tr("Turn Enhanced Mode on or off"), func(k *Kumo) { k.setTun(!k.prefs.Core.Tun) }},
		{"mode", tr("Next mode: Rule, Global, Direct"), func(k *Kumo) {
			if !k.online {
				return
			}
			next := modeIDs[(max(slices.Index(modeIDs, k.cfg.Mode), 0)+1)%len(modeIDs)]
			k.setMode(next)
			k.say(tr("Mode: ") + modeLabel(next))
		}},
		{"window", tr("Show or hide Kumo"), func(k *Kumo) {
			if k.win.IsVisible() && !k.win.IsMinimized() {
				k.win.Hide()
			} else {
				k.show()
			}
		}},
	}
}

// hotkeyConflict names the other action already using acc, or "".
func hotkeyConflict(keys map[string]string, action, acc string) string {
	for _, a := range hotkeyActions() {
		if a.id != action && acc != "" && strings.EqualFold(keys[a.id], acc) {
			return a.title
		}
	}
	return ""
}

// registerHotkeys registers the saved shortcuts anew, noting those the
// system refuses (taken by another app, say).
func (k *Kumo) registerHotkeys() {
	mygo.GlobalShortcut.UnregisterAll()
	k.hotkeyErr = map[string]string{}
	for _, a := range hotkeyActions() {
		acc := k.prefs.App.Hotkeys[a.id]
		if acc == "" {
			continue
		}
		run := a.run
		if err := mygo.GlobalShortcut.Register(acc, func() { run(k) }); err != nil {
			k.hotkeyErr[a.id] = err.Error()
		}
	}
}

// setHotkey saves acc ("" clears) for action, refusing one another action
// has, and registers the shortcuts again.
func (k *Kumo) setHotkey(action, acc string) {
	k.hotkeyRec = ""
	if other := hotkeyConflict(k.prefs.App.Hotkeys, action, acc); other != "" {
		k.say(fmt.Sprintf(tr("%s is already used for “%s”."), hotkeyLabel(acc, runtime.GOOS), other))
		k.registerHotkeys()
		return
	}
	if k.prefs.App.Hotkeys == nil {
		k.prefs.App.Hotkeys = map[string]string{}
	}
	if acc == "" {
		delete(k.prefs.App.Hotkeys, action)
	} else {
		k.prefs.App.Hotkeys[action] = acc
	}
	_ = k.prefs.save()
	k.registerHotkeys()
	if msg := k.hotkeyErr[action]; msg != "" {
		k.say(fmt.Sprintf(tr("%s could not be registered; another app may use it."), hotkeyLabel(acc, runtime.GOOS)))
	}
}
