package main

import (
	_ "embed"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
)

// trayGlyph is Kumo's cloud in black on transparent, for the macOS menu bar.
//
//go:embed art/tray.png
var trayGlyph []byte

// installAppMenu gives macOS its menu bar: About, Hide, Quit (⌘Q), and the
// standard Edit and Window menus. Other systems have none.
func installAppMenu() {
	if runtime.GOOS != "darwin" {
		return
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Kumo", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			mygo.Separator(),
			{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	}))
}

// installTray puts Kumo in the menu bar or notification area. With a tray,
// closing the window only hides it; quitting is explicit.
func (k *Kumo) installTray() {
	icon, template := trayGlyph, true
	if runtime.GOOS != "darwin" {
		if dir, err := mygo.App.Path(mygo.PathResources); err == nil {
			if png, err := os.ReadFile(filepath.Join(dir, "icon.png")); err == nil {
				icon, template = png, false
			}
		}
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: icon, IconIsTemplate: template, ToolTip: "Kumo", Menu: mygo.NewMenu(k.native(k.quickMenu(false)))})
	if err != nil {
		log.Printf("tray unavailable: %v", err)
		// KUMO_HIDE_ON_CLOSE keeps the hide-on-close behavior without a
		// tray, so tests can check quitting the way users meet it.
		if os.Getenv("KUMO_HIDE_ON_CLOSE") == "" {
			return
		}
	}
	k.tray = tray
	k.win.OnClose(func(e *mygo.CloseEvent) {
		if !k.quitting {
			e.PreventDefault()
			k.win.Hide()
		}
	})
	mygo.App.OnActivate(func(bool) { k.show() })
}
