package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kumo/update"
)

// The states of the updater.
const (
	updIdle        = iota
	updChecking    // looking for a new version
	updUpToDate    // none newer
	updAvailable   // one is offered
	updDownloading // fetching and verifying it
	updInstalling  // handing over to the helper
	updManual      // downloaded, the user installs it (.deb)
	updFailed
)

// appUpdate is the state of the in-app updater.
type appUpdate struct {
	state       int
	latest      *update.Manifest
	err         string
	done, total int64
	open        bool // the update dialog is shown
	cancel      context.CancelFunc
	manualFile  string // the verified .deb
	install     update.Install
}

const updateEvery = 24 * time.Hour

// updateWork is the updater's folder: downloads, the helper, its log.
func (k *Kumo) updateWork() string { return filepath.Join(k.dataDir, "updates") }

// helperWork is the helper's own folder, where it leaves its result.
func (k *Kumo) helperWork() string { return filepath.Join(k.updateWork(), "helper") }

func (k *Kumo) feed() *update.Feed {
	f := &update.Feed{Repo: "kumo-app/kumo", UserAgent: "Kumo/" + version}
	// For testing against a local copy of GitHub. Manifests and files are
	// still checked against the built-in key, wherever they come from.
	f.Web, f.API = os.Getenv("KUMO_UPDATE_WEB"), os.Getenv("KUMO_UPDATE_API")
	// While Kumo is connected, go through its own port, as other apps do.
	if k.proxyOn {
		if port := k.listenPort(); port > 0 {
			u := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}
			f.Proxy = func(*http.Request) (*url.URL, error) { return u, nil }
		}
	}
	return f
}

func (k *Kumo) channel() string {
	if k.prefs.App.UpdateChannel == update.Beta {
		return update.Beta
	}
	return update.Stable
}

// startUpdates reports the last update, then checks at launch and every
// day after, while the setting is on.
func (k *Kumo) startUpdates() {
	update.ReportStarted()
	// The helper writes its result once this Kumo has reported in.
	go func() {
		for range 60 {
			if r, ok := update.TakeResult(k.helperWork()); ok {
				k.post(func() { k.reportUpdate(r) })
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	}()
	go func() {
		time.Sleep(8 * time.Second)
		k.post(func() { k.checkUpdate(false) })
		for range time.Tick(time.Hour) {
			k.post(func() {
				if time.Since(time.Unix(k.prefs.App.LastUpdateCheck, 0)) >= updateEvery {
					k.checkUpdate(false)
				}
			})
		}
	}()
}

// reportUpdate tells how the last update went.
func (k *Kumo) reportUpdate(r *update.Result) {
	switch {
	case r.OK:
		k.say(fmt.Sprintf(tr("Kumo was updated to %s."), r.Version))
	case r.RolledBack:
		k.say(fmt.Sprintf(tr("The update to %s failed and Kumo %s was kept: %s"), r.Version, version, r.Error))
	default:
		k.say(fmt.Sprintf(tr("The update to %s failed: %s"), r.Version, r.Error))
	}
}

// checkUpdate looks for a newer Kumo; manual checks (Check now) also
// offer a skipped version and say when there is none.
func (k *Kumo) checkUpdate(manual bool) {
	u := &k.upd
	if !manual && k.prefs.App.NoUpdateCheck {
		return
	}
	if u.state == updChecking || u.state == updDownloading || u.state == updInstalling {
		return
	}
	u.state, u.err = updChecking, ""
	f, ch := k.feed(), k.channel()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		m, err := f.Latest(ctx, ch)
		k.post(func() {
			k.prefs.App.LastUpdateCheck = time.Now().Unix()
			_ = k.prefs.save()
			switch {
			case err != nil:
				u.state, u.err = updFailed, err.Error()
				if manual {
					k.say(tr("Could not check for updates: ") + err.Error())
				}
			case !update.Newer(m.Version, version):
				u.state, u.latest = updUpToDate, nil
				if manual {
					k.say(fmt.Sprintf(tr("Kumo %s is the latest version."), version))
				}
			case !manual && m.Version == k.prefs.App.SkipVersion:
				u.state, u.latest = updIdle, m
			default:
				u.state, u.latest, u.open = updAvailable, m, true
			}
		})
	}()
}

// startDownload fetches the new version in the background, verifies it,
// and installs it.
func (k *Kumo) startDownload() {
	u := &k.upd
	m := u.latest
	if m == nil {
		return
	}
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
	}
	in := update.Detect(exe, runtime.GOOS)
	in.CheckWritable()
	u.install = in
	key := update.Key(runtime.GOOS, runtime.GOARCH, in.Kind)
	a, ok := m.Assets[key]
	if !ok || (in.Manual != "" && in.Kind != update.LinuxDeb) {
		u.state, u.err = updFailed, manualReason(in.Manual)
		if !ok {
			u.state, u.err = updFailed, tr("This release has no download for this computer.")
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.state, u.err, u.done, u.total = updDownloading, "", 0, a.Size
	f, work, helper, ver := k.feed(), k.updateWork(), k.helperWork(), m.Version
	go func() {
		last := time.Time{}
		path, err := f.Download(ctx, a, filepath.Join(work, "download"), func(done, total int64) {
			if time.Since(last) < 100*time.Millisecond && done < total {
				return
			}
			last = time.Now()
			k.post(func() { u.done, u.total = done, total })
		})
		if err != nil {
			k.post(func() {
				if ctx.Err() != nil {
					u.state = updAvailable
					return
				}
				u.state, u.err = updFailed, tr("The download failed or did not verify, and was discarded: ")+err.Error()
			})
			return
		}
		if in.Kind == update.LinuxDeb {
			k.post(func() {
				u.state, u.manualFile = updManual, path
				_ = mygo.Shell.OpenPath(path)
			})
			return
		}
		plan, err := update.Prepare(in, path, ver, helper)
		if err == nil && in.Kind != update.WinSetup {
			os.Remove(path) // unpacked beside the install
		}
		k.post(func() {
			if err != nil {
				u.state, u.err = updFailed, err.Error()
				return
			}
			k.installUpdate(plan)
		})
	}()
}

// installUpdate starts the helper and quits: quitting restores the system
// proxy and stops the core, as always, before the helper swaps the files
// and starts the new version (or puts this one back if that fails).
func (k *Kumo) installUpdate(plan *update.Plan) {
	u := &k.upd
	u.state = updInstalling
	if err := update.Start(plan); err != nil {
		u.state, u.err = updFailed, err.Error()
		return
	}
	k.quitting = true
	mygo.App.Quit()
}

func manualReason(r string) string {
	switch r {
	case update.ReasonTranslocated:
		return tr("macOS runs Kumo from a temporary read-only copy. Move Kumo to the Applications folder, open it from there, and try again.")
	case update.ReasonReadOnly:
		return tr("Kumo cannot write to the folder it is installed in. Download the new version and install it yourself.")
	case update.ReasonPackage:
		return tr("Kumo was installed by the system's package manager.")
	}
	return tr("This copy of Kumo cannot update itself. Download the new version and install it yourself.")
}

func (k *Kumo) skipUpdate() {
	if k.upd.latest != nil {
		k.prefs.App.SkipVersion = k.upd.latest.Version
		_ = k.prefs.save()
	}
	k.upd.state, k.upd.open = updIdle, false
}

// updateDialog offers a new version: its notes, then the download's
// progress. Escape or clicking outside means Later; a download carries on.
func (k *Kumo) updateDialog(c *ui.Context, s skin) {
	u := &k.upd
	if u.latest == nil {
		u.open = false
		return
	}
	m := u.latest
	ui.Modal(c, &u.open, func() {
		ui.Column(c).Width(480).Gap(12).Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, glyph("cloud-download")).FontSize(26).TextColor(s.accent)
				ui.Column(c).Gap(2).Grow(1).Children(func() {
					ui.Text(c, fmt.Sprintf(tr("Kumo %s is available"), m.Version)).FontSize(18).Bold()
					sub := fmt.Sprintf(tr("You have %s."), version)
					if m.Channel == update.Beta {
						sub += " " + tr("Beta release.")
					}
					ui.Text(c, sub).FontSize(13).TextColor(s.muted)
				})
			})
			notes := strings.TrimSpace(m.Notes)
			if notes == "" {
				notes = tr("No release notes.")
			}
			ui.Scroll(c).MaxHeight(260).Padding(10, 12).Radius(8).Background(s.hover).Children(func() {
				ui.Text(c, notes).FontSize(13).LineHeight(1.45).Selectable()
			})
			if k.prefs.Core.Tun {
				ui.Text(c, tr("Enhanced Mode is on: after the update Kumo asks for an administrator's authorization again, because its helper only trusts the exact build that set it up.")).
					FontSize(12.5).TextColor(slow).LineHeight(1.4)
			}
			k.updateStatus(c, s)
			ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				k.updateButtons(c, s, true)
			})
		})
	})
}

// updateStatus shows the progress or the error of an update.
func (k *Kumo) updateStatus(c *ui.Context, s skin) {
	u := &k.upd
	switch u.state {
	case updDownloading:
		frac := -1.0
		if u.total > 0 {
			frac = float64(u.done) / float64(u.total)
		}
		ui.Column(c).Gap(6).Children(func() {
			ui.Progress(c, frac).Label(tr("Downloading"))
			ui.Text(c, fmt.Sprintf(tr("Downloading… %s of %s"), size(u.done), size(u.total))).FontSize(12.5).TextColor(s.muted)
		})
	case updInstalling:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Size(16, 16)
			ui.Text(c, tr("Verified. Kumo restores the system proxy, quits and restarts as the new version…")).FontSize(12.5).TextColor(s.muted)
		})
	case updManual:
		ui.Text(c, fmt.Sprintf(tr("Downloaded and verified. Install it with your package manager, or run: sudo apt install %s"), u.manualFile)).
			FontSize(12.5).TextColor(s.muted).LineHeight(1.4).Selectable()
	case updFailed:
		if u.err != "" {
			ui.Text(c, u.err).FontSize(12.5).TextColor(slow).LineHeight(1.4).Selectable()
		}
	}
}

// updateButtons are the dialog's choices, or the Settings row's.
func (k *Kumo) updateButtons(c *ui.Context, s skin, dialog bool) {
	u := &k.upd
	switch u.state {
	case updDownloading:
		if ui.Button(c, tr("Cancel")).Clicked() && u.cancel != nil {
			u.cancel()
		}
		if dialog && ui.PrimaryButton(c, tr("Hide")).Clicked() {
			u.open = false
		}
	case updInstalling:
	case updManual, updFailed:
		if u.latest != nil && ui.Button(c, tr("Open download page")).Clicked() {
			_ = mygo.Shell.OpenExternal(u.latest.Page)
		}
		if dialog && ui.PrimaryButton(c, tr("Close")).Clicked() {
			u.open = false
		}
		if !dialog && u.latest != nil && u.state == updFailed && ui.Button(c, tr("Try again")).Clicked() {
			k.startDownload()
		}
	default:
		if dialog {
			if ui.Button(c, tr("Skip this version")).Clicked() {
				k.skipUpdate()
			}
			if ui.Button(c, tr("Later")).Clicked() {
				u.open = false
			}
		}
		if ui.PrimaryButton(c, tr("Update now")).Clicked() {
			k.startDownload()
			u.open = true
		}
	}
}

// updateSettings is the Updates section of Settings.
func (k *Kumo) updateSettings(c *ui.Context, s skin) {
	u := &k.upd
	section(c, s, tr("UPDATES"))
	setting(c, s, tr("Check for updates automatically"), tr("At launch and once a day. Updates are signed; Kumo installs one only after you agree."), func() {
		on := !k.prefs.App.NoUpdateCheck
		if ui.Switch(c, &on).Label(tr("Check for updates automatically")).Changed() {
			k.prefs.App.NoUpdateCheck = !on
			_ = k.prefs.save()
		}
	})
	setting(c, s, tr("Update channel"), tr("Beta gets pre-releases before everyone else."), func() {
		i := 0
		if k.channel() == update.Beta {
			i = 1
		}
		if ui.Segmented(c, &i, tr("Stable"), tr("Beta")).Label(tr("Update channel")).Changed() {
			k.prefs.App.UpdateChannel = []string{update.Stable, update.Beta}[i]
			_ = k.prefs.save()
		}
	})
	sp := " " // between sentences; Chinese needs none
	if lang == "zh" {
		sp = ""
	}
	detail := fmt.Sprintf(tr("Kumo %s."), version)
	if t := k.prefs.App.LastUpdateCheck; t > 0 {
		detail += sp + fmt.Sprintf(tr("Last checked %s."), time.Unix(t, 0).Format("2006-01-02 15:04"))
	}
	switch {
	case u.state == updUpToDate:
		detail += sp + tr("Up to date.")
	case u.latest != nil && u.state != updChecking:
		detail += sp + fmt.Sprintf(tr("Version %s is available."), u.latest.Version)
	}
	setting(c, s, tr("Software update"), detail, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if u.state == updChecking {
				ui.Spinner(c).Size(16, 16)
			}
			if u.latest != nil && u.state != updChecking && u.state != updUpToDate {
				if u.state == updAvailable || u.state == updIdle {
					if ui.Button(c, tr("Show")).Clicked() {
						u.open = true
					}
				} else {
					k.updateButtons(c, s, false)
				}
			}
			if ui.Button(c, tr("Check now")).Clicked() {
				k.checkUpdate(true)
			}
		})
	})
	if u.state == updDownloading || u.state == updFailed || u.state == updManual || u.state == updInstalling {
		ui.Column(c).Padding(8, 0).Children(func() { k.updateStatus(c, s) })
	}
}
