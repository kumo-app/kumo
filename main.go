// Kumo is a desktop client for the mihomo (Clash) proxy core, drawn
// natively with MyGo. It bundles and runs the core, switches the system
// proxy, and shows the core's live state through its external controller.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kumo/core"
	"kumo/tun"
	"kumo/update"
)

func main() {
	// Kumo's executable also serves as the core's watchdog and as the
	// client and controller filter of the macOS Enhanced Mode helper. These checks come before
	// anything else, so that neither ever starts the GUI.
	if pid := os.Getenv(core.WatchdogEnv); pid != "" {
		core.RunWatchdog(pid)
		return
	}
	if path := os.Getenv(tun.CheckEnv); path != "" {
		os.Exit(tun.RunCheck(path))
	}
	if sock := os.Getenv(tun.FilterEnv); sock != "" {
		os.Exit(tun.RunFilter(sock))
	}
	if root := os.Getenv(tun.ClientEnv); root != "" {
		os.Exit(tun.RunClient(root, os.Args[1:]))
	}
	if plan := os.Getenv(update.PlanEnv); plan != "" {
		os.Exit(update.RunHelper(plan))
	}
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	base, err := mygo.App.Path(mygo.PathAppData)
	if err != nil {
		log.Fatal(err)
	}
	dataDir := filepath.Join(base, "Kumo")
	if d := os.Getenv("KUMO_DATA"); d != "" {
		dataDir = d
	}
	mygo.App.SetPath(mygo.PathUserData, dataDir)
	prefs := loadPrefs(filepath.Join(dataDir, "settings.json"))
	lang = prefs.App.Language
	applyTheme(prefs)
	installAppMenu()

	var k *Kumo
	mygo.App.OnSecondInstance(func([]string, string) {
		if k != nil {
			k.show()
		}
	})
	mygo.App.WhenReady(func() {
		// The first launch follows the system language, kept from then on
		// so that the interface and Settings agree.
		if l := chooseLanguage(prefs.App.Language, mygo.App.Locale()); l != prefs.App.Language {
			prefs.App.Language = l
			_ = prefs.save()
		}
		lang = prefs.App.Language
		tun.Prompt = tr("Kumo is installing its Enhanced Mode helper and a root-owned copy of the mihomo core.")
		var mgr *core.Manager
		bundled := ""
		if res, err := mygo.App.Path(mygo.PathResources); err == nil {
			bundled = core.FindBundled(res)
			mgr = &core.Manager{Binary: core.FindBinary(res), Dir: filepath.Join(dataDir, "core"), Watchdog: true}
			reclaim(mgr.Dir)
		}
		k = newKumo(prefs, dataDir, mgr, core.LoadProfiles(filepath.Join(dataDir, "profiles")))
		k.tunSrc = bundled
		if mgr != nil && runtime.GOOS == "windows" {
			// The core can only be killed there: take TUN's routes down first.
			mgr.BeforeStop = k.tunDown
		}
		// Every quit (⌘Q, the menus, SIGTERM) passes here before windows
		// are asked to close, so a hiding window cannot cancel it.
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { k.quitting = true })
		mygo.App.OnQuit(k.shutdown)
		if p := os.Getenv("KUMO_PAGE"); p != "" {
			k.router.Replace(p)
		}
		w, h := 1180, 760
		fmt.Sscanf(os.Getenv("KUMO_WINDOW"), "%dx%d", &w, &h)
		k.win = mygo.NewWindow(mygo.WindowOptions{
			Title:                "Kumo",
			Width:                w,
			Height:               h,
			MinWidth:             900,
			MinHeight:            600,
			StateKey:             "main",
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
			TitleBarHeight:       44,
			AutoHideMenuBar:      true,
			Content:              ui.View(k.view),
		})
		k.installTray()
		k.startUpdates()
		if runtime.GOOS == "darwin" {
			k.dockMenu = true
			k.refreshMenus()
		}
		k.repairProxy()
		if mgr != nil {
			mgr.ReapOrphan()
		}
		k.startFeatures()
		k.startCore()
		k.checkLegacyTun()
		k.reportLoadErrors()
		go func() {
			for range time.Tick(10 * time.Minute) {
				k.post(k.updateDue)
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (k *Kumo) show() {
	k.win.Show()
	k.win.Focus()
}
