package main

import (
	"net"
	"strconv"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"kumo/clash"
	"kumo/core"
	"kumo/sysproxy"
	"kumo/tun"
)

// bundled says that Kumo runs its own core rather than an external one.
func (k *Kumo) bundled() bool { return k.mgr != nil && !k.prefs.Core.External }

// target is the controller Kumo talks to.
func (k *Kumo) target() (string, int, string) {
	if k.bundled() {
		return "127.0.0.1", k.prefs.Core.Port, k.prefs.Core.Secret
	}
	c := k.prefs.Controller
	if k.prefs.envSecret != "" {
		return c.Host, c.Port, k.prefs.envSecret
	}
	return c.Host, c.Port, c.Secret
}

func (k *Kumo) runtimeFile() string { return filepath.Join(k.mgr.Dir, "runtime.yaml") }

func (k *Kumo) overrides() core.Overrides {
	c := k.prefs.Core
	return core.Overrides{
		Controller: fmt.Sprintf("127.0.0.1:%d", c.Port), Secret: c.Secret,
		MixedPort: c.MixedPort, Tun: c.Tun, AllowLan: c.AllowLan, TestPort: c.TestPort,
	}
}

// buildRuntime writes the configuration the core runs: the active profile
// with Kumo's overrides, or the built-in direct profile when the active
// one fails the core's test. It returns the proxy port. It runs off the
// main thread and touches only its arguments.
func buildRuntime(mgr *core.Manager, profiles *core.Profiles, path string, ov core.Overrides) (int, error) {
	err := core.WriteRuntime(path, profiles.ActiveYAML(), ov)
	if err == nil {
		err = mgr.Test(path)
	}
	if err != nil {
		if e := core.WriteRuntime(path, []byte(core.DefaultProfile), ov); e != nil {
			return 0, e
		}
	}
	data, _ := os.ReadFile(path)
	return core.MixedPort(data), err
}

// coreMu runs the core's (re)configurations one at a time, newest wins:
// each takes a number from coreSeq and gives up when a newer one exists,
// so the core never runs a profile or TUN state older than the UI shows.
var coreMu sync.Mutex

func (k *Kumo) latest(seq int64) bool { return k.coreSeq.Load() == seq }

// plainCore is the binary without Enhanced Mode.
func (k *Kumo) plainCore() string { return k.coreBin }

// enhanced says how to run the core with TUN, false when that needs the
// user's authorization first. Only the bundled core is ever privileged.
func (k *Kumo) enhanced() (tun.Launch, bool) {
	if k.tunSrc == "" {
		return tun.Launch{}, false
	}
	return tunCore(k.tunSrc)
}

// startCore starts (or restarts) the bundled core and connects to it; with
// an external controller it only connects.
func (k *Kumo) startCore() {
	if !k.bundled() {
		if k.mgr != nil {
			go k.mgr.Stop()
		}
		k.connect()
		return
	}
	if k.prefs.Core.Secret == "" {
		k.prefs.Core.Secret = core.NewSecret()
	}
	k.prefs.Core.Port = core.FreePort(k.prefs.Core.Port)
	k.prefs.Core.TestPort = core.FreePort(k.prefs.Core.TestPort)
	if k.prefs.Core.TestPort == k.prefs.Core.Port || k.prefs.Core.TestPort == k.prefs.Core.MixedPort {
		k.prefs.Core.TestPort = core.FreePort(0)
	}
	_ = k.prefs.save()
	k.connect()
	seq := k.coreSeq.Add(1)
	mgr, profiles, path, ov, plain, tunSrc := k.mgr, k.profiles, k.runtimeFile(), k.overrides(), k.plainCore(), k.tunSrc
	askVersion := k.coreVersion == ""
	go func() {
		coreMu.Lock()
		defer coreMu.Unlock()
		if !k.latest(seq) {
			return
		}
		launch, ok := tun.Launch{}, false
		if tunSrc != "" {
			launch, ok = tunCore(tunSrc)
		}
		reauth := ov.Tun && !ok
		if ov.Tun && ok {
			mgr.Use(launch.Binary, launch.Env...)
		} else {
			mgr.Use(plain)
			ov.Tun = false
		}
		if askVersion {
			v := mgr.Version()
			k.post(func() { k.coreVersion = v })
		}
		var port int
		var err, startErr error
		// A port found free can be taken before the core listens on it;
		// then the core gets another.
		for attempt := 0; attempt < 3; attempt++ {
			port, err = buildRuntime(mgr, profiles, path, ov)
			if !k.latest(seq) {
				return
			}
			k.freshCore.Store(true)
			startErr = mgr.Start(path)
			if startErr != nil || !controllerTaken(mgr, ov.Controller) {
				break
			}
			free := core.FreePort(0)
			ov.Controller = fmt.Sprintf("127.0.0.1:%d", free)
			k.post(func() {
				k.prefs.Core.Port = free
				_ = k.prefs.save()
				k.connect()
			})
		}
		k.post(func() {
			k.proxyPort = port
			if err != nil {
				k.say(tr("The subscription could not be loaded; running direct. ") + err.Error())
			}
			if startErr != nil {
				k.say(tr("The core could not start: ") + startErr.Error())
			}
			if reauth {
				k.askReauthorize()
			}
		})
	}()
}

// controllerTaken waits briefly for the core and says whether it ended
// because the controller's port was taken.
func controllerTaken(mgr *core.Manager, addr string) bool {
	for range 15 {
		st, msg := mgr.Status()
		if st == core.Failed {
			return strings.Contains(msg, "address already in use") && strings.Contains(msg, addr)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// onOnline runs each time the controller answers after not answering.
func (k *Kumo) onOnline() {
	if k.prefs.Core.AutoSystemProxy && !k.autoDone {
		k.autoDone = true
		k.setSystemProxy(true)
	}
}

// listenPort is the port the system proxy points at.
func (k *Kumo) listenPort() int {
	for _, p := range []int{k.cfg.MixedPort, k.cfg.Port, k.proxyPort, k.prefs.Core.MixedPort} {
		if p > 0 {
			return p
		}
	}
	return 7890
}

// The system proxy. Kumo keeps the user's own settings in a backup file
// while its own are in force, so that disconnecting, quitting, or the next
// launch after a crash puts them back.

var proxyMu sync.Mutex

func (k *Kumo) backupFile() string { return filepath.Join(k.dataDir, "sysproxy-backup.json") }

func proxyOn(backup string, port int) error {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	if _, saved := sysproxy.LoadBackup(backup); !saved {
		st, err := sysproxy.Snapshot()
		if err != nil {
			return err
		}
		if err := sysproxy.SaveBackup(backup, st); err != nil {
			return err
		}
	}
	if err := sysproxy.Set("127.0.0.1", port); err != nil {
		if st, ok := sysproxy.LoadBackup(backup); ok && sysproxy.Restore(st) == nil {
			_ = os.Remove(backup)
		}
		return err
	}
	return nil
}

// proxyOff puts the user's settings back. Without a backup it only turns
// off proxies that point at Kumo's port, never the user's own.
func proxyOff(backup string, port int) error {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	st, saved := sysproxy.LoadBackup(backup)
	if !saved {
		return sysproxy.ClearOurs("127.0.0.1", port)
	}
	// A backup taken while another client (or a Kumo that crashed) had
	// pointed the system at this computer is put back as off where nothing
	// listens there any more: such a proxy would block every app.
	st, _ = st.OffWhereDead(portAlive)
	if err := sysproxy.Restore(st); err != nil {
		return err
	}
	return os.Remove(backup)
}

// portAlive reports whether something listens on port of this computer.
func portAlive(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 400*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// repairProxy restores settings a crashed Kumo left behind.
func (k *Kumo) repairProxy() {
	backup := k.backupFile()
	if _, saved := sysproxy.LoadBackup(backup); !saved {
		return
	}
	go func() {
		if err := proxyOff(backup, 0); err == nil {
			k.post(func() { k.say(tr("Kumo did not quit cleanly last time; your previous proxy settings were restored.")) })
		}
	}()
}

func (k *Kumo) setSystemProxy(on bool) {
	if k.proxyBusy {
		return
	}
	port, backup := k.listenPort(), k.backupFile()
	k.proxyBusy = true
	go func() {
		var err error
		if on {
			err = proxyOn(backup, port)
		} else {
			err = proxyOff(backup, port)
		}
		k.post(func() {
			k.proxyBusy = false
			if err != nil {
				k.proxyErr = explainProxyError(err, port)
				k.proxyOn = false
				k.proxyApplied(false, 0)
				k.say(k.proxyErr)
			} else {
				k.proxyErr, k.proxyOn, k.proxyPort = "", on, port
				k.proxyApplied(on, port)
				if on {
					k.say(tr("Connected — apps now go through Kumo"))
				} else {
					k.say(tr("Disconnected — system proxy restored"))
				}
			}
			k.refreshMenus()
		})
	}()
}

// tunCore is the binary that may open a TUN device, or "" before the
// user has authorized one; a variable so that tests can stand in.
var tunCore = tun.Core

// tunInstall asks the user once for the rights TUN needs.
var tunInstall = tun.Install

// setTun turns Enhanced Mode (mihomo's TUN) on or off. Turning it on the
// first time explains what will be installed, asks for an administrator's
// authorization, then restarts the core with the privileged launch.
// Turning it off restarts the core without any privilege.
func (k *Kumo) setTun(on bool) {
	if k.tunBusy {
		return
	}
	if !k.bundled() {
		// An external core has rights of its own; ask it.
		k.prefs.Core.Tun = on
		_ = k.prefs.save()
		k.patch(map[string]any{"tun": map[string]any{"enable": on}})
		return
	}
	if !on {
		k.prefs.Core.Tun = false
		_ = k.prefs.save()
		k.startCore()
		k.refreshMenus()
		return
	}
	if _, ok := k.enhanced(); ok {
		k.enableTun()
		return
	}
	if k.tunSrc == "" {
		k.say(explainTunError(tun.ErrUntrusted))
		return
	}
	src := k.tunSrc
	k.tunBusy = true
	k.refreshMenus()
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: k.win, Type: mygo.MessageInfo,
			Message: tr("Turn on Enhanced Mode?"), Detail: tunPlan(),
			Buttons: []string{tr("Continue"), tr("Cancel")}, DefaultButton: 0, CancelButton: 1})
		if err == nil && res.Button == 0 {
			err = tunInstall(src)
		} else if err == nil {
			err = tun.ErrDenied
		}
		k.post(func() {
			k.tunBusy = false
			if err != nil {
				if !errors.Is(err, tun.ErrDenied) || res.Button == 0 {
					k.say(explainTunError(err))
				}
				k.refreshMenus()
				return
			}
			k.enableTun()
		})
	}()
}

func (k *Kumo) enableTun() {
	k.prefs.Core.Tun = true
	_ = k.prefs.save()
	k.say(tr("Enhanced Mode on: all traffic goes through Kumo."))
	k.startCore()
	k.refreshMenus()
}

// askReauthorize tells the user, once a session, that Enhanced Mode is
// set but not authorized (after an update of Kumo or its core, say), and
// lets them set it up again or turn it off.
func (k *Kumo) askReauthorize() {
	if k.tunAsked || k.tunBusy {
		return
	}
	k.tunAsked = true
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: k.win, Type: mygo.MessageWarning,
			Message: tr("Enhanced Mode needs to be set up again"),
			Detail:  tr("Kumo or its core changed since you authorized Enhanced Mode, so Kumo now runs without it: apps that ignore the system proxy bypass Kumo."),
			Buttons: reauthButtons(), DefaultButton: 0, CancelButton: reauthLater})
		button := reauthLater
		if err == nil {
			button = res.Button
		}
		k.post(func() {
			switch button {
			case reauthSetUp:
				k.setTun(true)
			case reauthTurnOff:
				k.setTun(false)
			}
			// Later (also Escape, closing the dialog, or an error) keeps
			// the setting: the core runs without Enhanced Mode now and
			// Kumo asks again at the next start.
		})
	}()
}

const (
	reauthSetUp = iota
	reauthTurnOff
	reauthLater
)

func reauthButtons() []string {
	return []string{tr("Set up again"), tr("Turn off Enhanced Mode"), tr("Later")}
}

// tunOn says whether Enhanced Mode is on in the running core.
func (k *Kumo) tunOn() bool { return k.online && k.cfg.Tun.Enable }

func (k *Kumo) setAllowLan(on bool) {
	k.prefs.Core.AllowLan = on
	_ = k.prefs.save()
	k.patch(map[string]any{"allow-lan": on})
}

// Subscriptions.

var autoUpdateChoices = []string{"Never", "Every 6 hours", "Every 12 hours", "Every 24 hours"}
var autoUpdateHours = []int{0, 6, 12, 24}

// useProfile makes id active and has the running core load it.
func (k *Kumo) useProfile(id string) {
	_ = k.profiles.SetActive(id)
	if !k.bundled() {
		return
	}
	cli, gen := k.cli, k.gen
	seq := k.coreSeq.Add(1)
	mgr, profiles, path, ov, tunSrc := k.mgr, k.profiles, k.runtimeFile(), k.overrides(), k.tunSrc
	go func() {
		coreMu.Lock()
		defer coreMu.Unlock()
		if !k.latest(seq) {
			return
		}
		if ov.Tun {
			_, ok := tunCore(tunSrc)
			ov.Tun = ok && tunSrc != ""
		}
		_, err := buildRuntime(mgr, profiles, path, ov)
		if err == nil {
			var data []byte
			if data, err = os.ReadFile(path); err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				err = cli.ReloadConfigs(ctx, data)
				cancel()
			}
		}
		k.post(func() {
			if k.gen != gen {
				return
			}
			if err != nil {
				k.say(tr("Could not switch subscription: ") + err.Error())
			}
			k.refresh()
		})
	}()
}

func (k *Kumo) addSubscription() {
	link := strings.TrimSpace(k.sub.url)
	if link == "" || k.sub.adding {
		return
	}
	name := strings.TrimSpace(k.sub.name)
	hours := autoUpdateHours[max(slices.Index(autoUpdateChoices, k.sub.every), 0)]
	k.sub.adding = true
	profiles := k.profiles
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		p, err := profiles.AddRemote(ctx, link, name, hours)
		k.post(func() {
			k.sub.adding = false
			if err != nil {
				k.say(explainSubError(err))
				return
			}
			k.sub.url, k.sub.name = "", ""
			k.useProfile(p.ID)
			msg := fmt.Sprintf(tr("Added “%s” and switched to it"), p.Name)
			if strings.HasPrefix(strings.ToLower(link), "http://") {
				msg += tr(". Note: this link is plain http, so your nodes and their passwords travel unencrypted; prefer https.")
			}
			k.say(msg)
		})
	}()
}

func (k *Kumo) importProfile() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: k.win, Title: tr("Import configuration"),
			Filters: []mygo.FileFilter{{Name: "YAML", Extensions: []string{"yaml", "yml"}}}})
		if err != nil || len(paths) == 0 {
			return
		}
		k.post(func() {
			p, err := k.profiles.AddLocal(paths[0])
			if err != nil {
				k.say(tr("Could not import the file: ") + explainSubError(err))
				return
			}
			k.useProfile(p.ID)
			k.say(fmt.Sprintf(tr("Added “%s” and switched to it"), p.Name))
		})
	}()
}

func (k *Kumo) updateProfile(id string) {
	if k.sub.updating[id] {
		return
	}
	k.sub.updating[id] = true
	profiles := k.profiles
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		err := profiles.Update(ctx, id)
		k.post(func() {
			delete(k.sub.updating, id)
			if err != nil {
				k.say(explainSubError(err))
				return
			}
			k.say(tr("Subscription updated"))
			if profiles.Active() == id {
				k.useProfile(id)
			}
		})
	}()
}

func (k *Kumo) removeProfile(p core.Profile) {
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: k.win, Type: mygo.MessageWarning,
			Message: fmt.Sprintf(tr("Delete “%s”?"), p.Name), Detail: tr("The subscription and its downloaded file are removed."),
			Buttons: []string{tr("Delete"), tr("Cancel")}, DefaultButton: 1, CancelButton: 1})
		if err != nil || res.Button != 0 {
			return
		}
		k.post(func() {
			wasActive := k.profiles.Active() == p.ID
			if err := k.profiles.Remove(p.ID); err != nil {
				k.say(err.Error())
				return
			}
			if wasActive {
				k.useProfile("")
			}
			k.say(tr("Subscription deleted"))
		})
	}()
}

func (k *Kumo) updateDue() {
	now := time.Now()
	for _, p := range k.profiles.List() {
		if p.Due(now) {
			k.updateProfile(p.ID)
		}
	}
}

// The cleanup when Kumo quits: the system proxy first, with a budget of
// its own, and only then the core, so that apps never point at a port
// nothing listens on.
const (
	proxyBudget = 8 * time.Second
	coreBudget  = 3 * time.Second
)

func (k *Kumo) shutdown() {
	if k.win != nil {
		k.win.Hide()
	}
	if k.cancel != nil {
		k.cancel()
	}
	backup, port := k.backupFile(), k.listenPort()
	if _, saved := sysproxy.LoadBackup(backup); saved || k.proxyOn || k.proxyBusy {
		within(proxyBudget, func() { _ = proxyOff(backup, port) })
	}
	if k.mgr != nil {
		within(coreBudget, k.mgr.Stop)
	}
	if k.stats != nil {
		_ = k.stats.Save()
	}
}

// within runs fn, waiting for it at most d.
func within(d time.Duration, fn func()) {
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
	}
}

// closeConnection closes one connection, or all when id is "".
func (k *Kumo) closeConnection(id string) {
	k.call(func(ctx context.Context, cli *clash.Client) error {
		if id == "" {
			return cli.CloseAllConnections(ctx)
		}
		return cli.CloseConnection(ctx, id)
	}, nil)
}

// confirmRestart restarts the core once the user agrees: it drops every
// open connection for a moment.
func (k *Kumo) confirmRestart() {
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: k.win, Type: mygo.MessageWarning,
			Message: tr("Restart the core?"),
			Detail:  tr("Open connections are closed and the configuration is rebuilt from the active subscription."),
			Buttons: []string{tr("Restart"), tr("Cancel")}, DefaultButton: 1, CancelButton: 1})
		if err != nil || res.Button != 0 {
			return
		}
		k.post(func() {
			k.startCore()
			k.say(tr("Restarting the core…"))
		})
	}()
}
