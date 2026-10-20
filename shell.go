package main

import (
	"time"

	"github.com/egoist/mygo/ui"

	"kumo/core"
)

// page is one screen of the window.
type page struct {
	path, icon, title string
	build             func(c *ui.Context, s skin)
}

func (k *Kumo) pages() []page {
	return []page{
		{"/home", "house", tr("Home"), k.homePage},
		{"/nodes", "globe", tr("Nodes"), k.nodesPage},
		{"/subscriptions", "folder-open", tr("Subscriptions"), k.subsPage},
		{"/rules", "list-filter", tr("Rules"), k.rulesPage},
		{"/connections", "cable", tr("Connections"), k.connsPage},
		{"/statistics", "chart-column", tr("Statistics"), k.statsPage},
		{"/logs", "scroll-text", tr("Logs"), k.logsPage},
		{"/core", "wrench", tr("Core"), k.corePage},
		{"/config", "file-code", tr("Configuration"), k.configPage},
		{"/settings", "settings", tr("Settings"), k.settingsPage},
	}
}

// Layout measures shared by the window's parts.
const (
	barHeight = 52  // the band of the window controls and the tools
	sideWidth = 200 // the sidebar, when shown
)

func (k *Kumo) view(c *ui.Context) {
	lang = k.prefs.App.Language
	s := k.dress(c)
	if k.notice != "" {
		c.Toast(k.notice)
		k.notice = ""
	}
	c.OnShortcut(ui.Cmd, ui.KeyR, k.reload)
	c.OnShortcut(ui.Cmd, ui.KeyComma, func() { k.router.Push("/settings") })
	c.Root().Background(s.bottom).TextColor(s.text)

	current := k.router.Path()
	if k.hotkeyRec != "" && current != "/settings" {
		k.recordHotkey("")
	}
	shown := k.pages()[0]
	for _, p := range k.pages() {
		if p.path == current {
			shown = p
		}
	}
	if !k.prefs.UI.AdvancedMode && (shown.path == "/core" || shown.path == "/config") {
		shown = k.pages()[0]
	}
	k.shown = shown.path
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Gradient(s.top, s.bottom, 180).Children(func() {
		k.sidebar(c, s, shown.path)
		area := ui.Column(c).Grow(1).MinWidth(0)
		if shown.path != "/home" {
			// Home's tiles lie on the window; other pages on a sheet.
			left := float32(0)
			if k.prefs.UI.SidebarHidden {
				left = 8
			}
			area.Margin(8, 8, 8, left).Radius(14).Background(s.sheet).Clip().Shadow(0, 1, 3, 0, s.shadow)
		}
		area.Children(func() { shown.build(c, s) })
	})
	k.tools(c, s)
	k.updateDialog(c, s)
	k.diagSheet(c, s)
	k.syncMenus()
}

// tools are the buttons beside the window controls.
func (k *Kumo) tools(c *ui.Context, s skin) {
	left := max(c.TitleBar().Left, 8) + 10
	ui.Row(c).Absolute().Top(0).Left(left).Height(barHeight).Gap(4).AlignItems(ui.Center).Children(func() {
		if roundButton(c, s, "panel-left", tr("Toggle sidebar")).Clicked() {
			k.prefs.UI.SidebarHidden = !k.prefs.UI.SidebarHidden
			_ = k.prefs.save()
		}
		if roundButton(c, s, "rotate-cw", tr("Refresh")).Clicked() {
			k.reload()
		}
	})
}

func (k *Kumo) sidebar(c *ui.Context, s skin, current string) {
	width := float32(sideWidth)
	if k.prefs.UI.SidebarHidden {
		width = 0
	}
	outer := ui.Column(c).Clip()
	outer.Width(outer.Animate("width", width, 180*time.Millisecond))
	outer.Children(func() {
		ui.Column(c).Width(sideWidth).Grow(1).Padding(barHeight+4, 10, 10, 10).Gap(4).DragWindow().Children(func() {
			pg := k.pages()
			for i, p := range pg[:7] {
				badge := 0
				if i == 4 {
					badge = len(k.conns.Connections)
				}
				k.navItem(c, s, p, current, badge)
			}
			if k.prefs.UI.AdvancedMode {
				k.navItem(c, s, pg[7], current, 0)
				k.navItem(c, s, pg[8], current, 0)
			}
			ui.Spacer(c)
			k.statusLine(c, s)
			ui.Divider(c).Background(s.line).Margin(4, 0)
			k.navItem(c, s, pg[9], current, 0)
		})
	})
}

// navItem is one row of the sidebar.
func (k *Kumo) navItem(c *ui.Context, s skin, p page, current string, badge int) {
	b := ui.ButtonBase(c.Key(p.path)).Height(38).Padding(0, 10).Gap(10).Radius(8).AlignItems(ui.Center).TextColor(s.text)
	switch {
	case p.path == current:
		b.Background(s.pick)
	case b.Hovered():
		b.Background(s.hover)
	}
	b.Children(func() {
		ui.Icon(c, glyph(p.icon)).FontSize(18)
		ui.Text(c, p.title).FontSize(14.5).FontWeight(500).SingleLine().Grow(1)
		if badge > 0 {
			ui.Text(c, itoa(badge)).FontSize(11).FontWeight(600).Padding(1, 6).Radius(9).
				Background(s.accent).TextColor(ui.RGB(255, 255, 255))
		}
	})
	if b.Clicked() && p.path != current {
		k.router.Push(p.path)
	}
}

// statusLine sums up the connection above Settings.
func (k *Kumo) statusLine(c *ui.Context, s skin) {
	color, text := ui.Hex("#ef4444"), tr("Core not running")
	switch {
	case k.online && k.proxyOn && k.proxyEff.stale():
		color, text = slow, tr("Proxy not in effect")
	case k.online && k.proxyOn:
		color, text = s.good, tr("Connected")
	case k.online:
		color, text = ui.Hex("#f59e0b"), tr("Ready · system proxy off")
	case k.bundled() && k.coreState != core.Failed:
		color, text = s.muted, tr("Starting core…")
	}
	ui.Row(c).Padding(6, 10).Gap(8).AlignItems(ui.Center).Children(func() {
		light(c, color)
		ui.Text(c, text).FontSize(12.5).TextColor(s.muted).SingleLine().Grow(1).MinWidth(0)
	})
}

// headRow is the row of a page's header, clear of the window controls.
func (k *Kumo) headRow(c *ui.Context, height, left float32) ui.Element {
	row := ui.Row(c)
	bar := c.TitleBar()
	top, right := float32(8), float32(24)
	if bar.Right > 0 {
		right = bar.Right + 12
	}
	if k.prefs.UI.SidebarHidden {
		top, right, left, height = barHeight-8, 24, max(left, 22), height+20
	}
	const d = 180 * time.Millisecond
	return row.Height(row.Animate("height", height, d)).
		Padding(row.Animate("top", top, d), right, 0, row.Animate("left", left, d))
}

// header is a sheet page's title band, with trailing controls, and the
// line under it.
func (k *Kumo) header(c *ui.Context, s skin, title string, trailing func()) {
	k.headRow(c, 76, 24).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
		ui.Text(c, title).FontSize(24).Bold().LetterSpacing(-0.4).SingleLine()
		if trailing != nil {
			trailing()
		}
	})
	ui.Divider(c).Background(s.line)
}

// sheetScroll is the scrolling body of a sheet page.
func sheetScroll(c *ui.Context, top float32, fn func()) {
	ui.Scroll(c).Grow(1).Padding(top, 24, 24, 24).Children(fn)
}

// waiting shows the empty state of a page while no core answers; it
// reports whether it did.
func (k *Kumo) waiting(c *ui.Context, s skin) bool {
	if k.online {
		return false
	}
	detail := tr("Waiting for the external controller at ") + k.prefs.Controller.Host + ":" + itoa(k.prefs.Controller.Port)
	if k.bundled() {
		detail = tr("Starting the bundled mihomo core…")
		if k.coreState == core.Failed {
			detail = tr("The bundled core failed: ") + k.coreErr
		}
	}
	if k.lastErr != "" {
		detail += "\n" + k.lastErr
	}
	blank(c, s, "unplug", tr("Disconnected"), detail)
	return true
}
