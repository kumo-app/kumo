package main

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kumo/core"
)

var modeIDs = []string{"rule", "global", "direct"}

func modeLabel(mode string) string {
	switch mode {
	case "global":
		return tr("Global")
	case "direct":
		return tr("Direct")
	case "rule":
		return tr("Rule")
	}
	return "—"
}

func modeHint(mode string) string {
	switch mode {
	case "global":
		return tr("All traffic via the node")
	case "direct":
		return tr("No proxy")
	}
	return tr("Split by rules · recommended")
}

func (k *Kumo) homePage(c *ui.Context, s skin) {
	k.headRow(c, 84, 10).AlignItems(ui.Center).DragWindow().Children(func() {
		ui.Text(c, tr("Home")).FontSize(24).Bold().LetterSpacing(-0.4).SingleLine()
		ui.Spacer(c)
		k.diagButton(c, s)
	})
	cols := 4
	if w, _ := c.Size(); w-k.sideShown() < 640 {
		cols = 2
	}
	ui.Scroll(c).Grow(1).Padding(4, 16, 24, 10).Children(func() {
		ui.Column(c).Gap(16).Children(func() {
			k.coreBanner(c, s)
			k.startBanner(c, s)
			ui.Grid(c).Columns(cols).Gap(16).Children(func() {
				k.proxyTile(c, s)
				k.chartTile(c, s, tr("UPLOAD"), k.upHist, s.up, s.upFill, 1, k.traffic.Up, "/s")
				k.chartTile(c, s, tr("DOWNLOAD"), k.dnHist, s.down, s.downFill, 1, k.traffic.Down, "/s")
				k.modeTile(c, s)
				k.nodeTile(c, s)
				k.chartTile(c, s, tr("MEMORY"), k.memHist, s.accent, s.accent.Alpha(0.16), 2, k.memory, "")
				k.connsTile(c, s)
				k.subTile(c, s)
			})
		})
	})
}

func (k *Kumo) sideShown() float32 {
	if k.prefs.UI.SidebarHidden {
		return 0
	}
	return sideWidth
}

// coreBanner shows only when the bundled core has failed.
func (k *Kumo) coreBanner(c *ui.Context, s skin) {
	if !k.bundled() || k.coreState != core.Failed {
		return
	}
	red := ui.Hex("#ef4444")
	ui.Row(c).Padding(10, 16).Radius(14).Gap(10).AlignItems(ui.Center).Background(red.Alpha(0.12)).Children(func() {
		ui.Icon(c, glyph("triangle-alert")).FontSize(16).TextColor(red)
		ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
			ui.Text(c, tr("Core not running")+" · "+explainCoreError(k.coreErr)).FontSize(13).FontWeight(600).SingleLine()
			ui.Text(c, k.coreErr).FontSize(11.5).TextColor(s.muted).SingleLine().Selectable()
		})
		if ui.Button(c, tr("View log")).Clicked() && k.mgr != nil {
			_ = mygo.Shell.OpenPath(k.mgr.LogPath())
		}
		if ui.PrimaryButton(c, tr("Restart")).Clicked() {
			k.startCore()
		}
	})
}

// startBanner is a slim hint while no subscription has been added.
func (k *Kumo) startBanner(c *ui.Context, s skin) {
	if !k.bundled() || len(k.profiles.List()) > 0 {
		return
	}
	ui.Row(c).Padding(10, 16).Radius(14).Gap(10).AlignItems(ui.Center).Background(s.accent.Alpha(0.10)).Children(func() {
		ui.Icon(c, glyph("link")).FontSize(16).TextColor(s.accent)
		ui.Text(c, tr("No subscription yet — running direct.")).FontSize(13).Grow(1).SingleLine()
		if ui.PrimaryButton(c, tr("Add subscription")).Clicked() {
			k.router.Push("/subscriptions")
		}
	})
}

// proxyTile is the system proxy switch with the connection's summary.
func (k *Kumo) proxyTile(c *ui.Context, s skin) {
	tile(c, s).ColumnSpan(2).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			label(c, s, tr("SYSTEM PROXY")).Grow(1)
			if k.proxyBusy {
				ui.Spinner(c).Size(16, 16)
			}
			on := k.proxyOn
			if ui.Switch(c, &on).Label(tr("System proxy")).Changed() && !k.proxyBusy {
				if k.online {
					k.setSystemProxy(on)
				} else {
					k.say(tr("The proxy core is still starting. Try again in a moment."))
				}
			}
		})
		state, color := tr("Not connected"), s.muted
		stale := k.proxyOn && k.proxyEff.stale()
		switch {
		case stale:
			state, color = tr("Not in effect"), slow
		case k.proxyOn:
			state, color = tr("Connected"), s.good
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(8, 0, 0, 0).Children(func() {
			light(c, color).Size(10, 10).Radius(5)
			ui.Text(c, state).FontSize(28).Bold().LetterSpacing(-0.5).SingleLine()
		})
		ui.Spacer(c)
		if k.proxyErr != "" {
			ui.Text(c, k.proxyErr).FontSize(12).TextColor(dead).MaxLines(2).Selectable()
			return
		}
		if stale {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, proxyCause(k.proxyEff)).FontSize(12).TextColor(slow).MaxLines(2).Selectable().Grow(1).Basis(0).MinWidth(0)
				if ui.Button(c, tr("Re-apply")).Tooltip(tr("Point the system proxy at Kumo again")).Clicked() {
					k.reapplyProxy()
				}
			})
			return
		}
		engine := "mihomo"
		if k.version.Version != "" {
			engine += " " + k.version.Version
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("127.0.0.1:%d   ·   %s   ·   %s", k.listenPort(), modeLabel(k.cfg.Mode), engine)).
				FontSize(12).TextColor(s.muted).FontFeatures("tnum").SingleLine().Selectable().Grow(1).MinWidth(0)
			k.tunSwitch(c, s)
		})
	})
}

// tunSwitch is Enhanced Mode's compact switch, captioned as the tile is.
func (k *Kumo) tunSwitch(c *ui.Context, s skin) {
	if k.tunBusy {
		ui.Spinner(c).Size(14, 14)
	}
	label(c, s, tr("ENHANCED MODE")).Tooltip(tr("Routes all traffic through Kumo with a TUN device, including apps that ignore the system proxy."))
	on := k.tunOn()
	if ui.Switch(c, &on).Label(tr("Enhanced Mode (TUN)")).Changed() {
		if k.online {
			k.setTun(on)
		} else {
			k.say(tr("The proxy core is still starting. Try again in a moment."))
		}
	}
}

// chartTile is a figure over its history.
func (k *Kumo) chartTile(c *ui.Context, s skin, title string, hist series, stroke, fill ui.Color, span int, value int64, suffix string) {
	tile(c, s).ColumnSpan(span).Children(func() {
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(84).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
			sparkline(p, r, hist, stroke, fill)
		})
		label(c, s, title)
		v, u := sizeParts(value)
		figure(c, v, u+suffix)
	})
}

func (k *Kumo) modeTile(c *ui.Context, s skin) {
	tile(c, s).Children(func() {
		label(c, s, tr("MODE"))
		i := max(slices.Index(modeIDs, k.cfg.Mode), 0)
		if ui.Segmented(c, &i, tr("Rule"), tr("Global"), tr("Direct")).Label(tr("Proxy mode")).FontSize(12).Margin(12, 0, 0, 0).Changed() {
			k.setMode(modeIDs[i])
			k.say(tr("Mode changed to ") + modeLabel(modeIDs[i]))
		}
		ui.Text(c, modeHint(modeIDs[i])).FontSize(12).TextColor(s.muted).Margin(8, 0, 0, 0).SingleLine()
		ui.Spacer(c)
		ui.Text(c, fmt.Sprintf("mixed :%d   socks :%d   http :%d", k.cfg.MixedPort, k.cfg.SocksPort, k.cfg.Port)).
			FontSize(12).TextColor(s.muted).FontFeatures("tnum").SingleLine()
	})
}

// nodeTile shows the node in use with its latency and switches it.
func (k *Kumo) nodeTile(c *ui.Context, s skin) {
	tile(c, s).Children(func() {
		g, ok := k.primaryGroup()
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			label(c, s, tr("NODE")).Grow(1)
			if ok && k.cfg.Mode != "direct" && roundButton(c, s, "zap", tr("Test speed")).Clicked() {
				k.measure(k.leaves(g.Name), nil)
			}
		})
		if k.cfg.Mode == "direct" || !ok || len(g.All) == 0 {
			ui.Text(c, tr("Direct")).FontSize(22).Bold().Margin(8, 0, 0, 0).SingleLine()
			ui.Spacer(c)
			ui.Text(c, tr("No node in direct mode")).FontSize(12).TextColor(s.muted).SingleLine()
			return
		}
		node := k.resolve(g.Name)
		ui.Text(c, node).FontSize(18).Bold().Margin(8, 0, 0, 0).SingleLine()
		p := k.proxies[node]
		ms, tested := p.LastDelay(), len(p.History) > 0
		ui.Text(c, latencyText(ms, tested)).FontSize(12.5).FontWeight(600).TextColor(latencyColor(s, ms, tested)).FontFeatures("tnum").SingleLine()
		ui.Spacer(c)
		choice := g.Now
		if ui.Select(c, &choice, g.All).Label(tr("Node")).Height(30).FontSize(12.5).Changed() {
			k.choose(g.Name, choice)
		}
	})
}

func (k *Kumo) subTile(c *ui.Context, s skin) {
	tile(c, s).ColumnSpan(2).Children(func() {
		label(c, s, tr("SUBSCRIPTION"))
		name, kind := tr("Default (direct)"), tr("Built-in")
		if p, ok := k.profiles.Find(k.profiles.Active()); ok {
			name, kind = p.Name, tr("Local")
			if p.Remote() {
				kind = tr("Subscription") + " · " + tr("updated ") + when(p.UpdatedAt)
			}
		}
		if !k.bundled() {
			name, kind = fmt.Sprintf("%s:%d", k.prefs.Controller.Host, k.prefs.Controller.Port), tr("External controller")
		}
		ui.Text(c, name).FontSize(22).Bold().LetterSpacing(-0.3).Margin(8, 0, 0, 0).SingleLine()
		ui.Text(c, kind).FontSize(12.5).TextColor(s.muted).SingleLine()
		ui.Spacer(c)
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, tr("Manage subscriptions")).Clicked() {
				k.router.Push("/subscriptions")
			}
		})
	})
}

func (k *Kumo) connsTile(c *ui.Context, s skin) {
	tile(c, s).ColumnSpan(2).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			label(c, s, tr("CONNECTIONS")).Grow(1)
			lamp := s.muted.Alpha(0.45)
			if len(k.conns.Connections) > 0 {
				lamp = s.accent
			}
			ui.Box(c).Size(10, 10).Radius(5).Background(lamp)
		})
		figure(c, itoa(len(k.conns.Connections)), "")
		ui.Spacer(c).MinHeight(18)
		ui.Row(c).Gap(24).Dividers(1, s.line).Children(func() {
			stat := func(value, name string) {
				ui.Column(c).Gap(2).Shrink(0).Children(func() {
					ui.Text(c, value).FontSize(20).Bold().FontFeatures("tnum").SingleLine()
					label(c, s, name)
				})
			}
			stat(size(k.conns.UploadTotal), tr("UPLOADED"))
			stat(size(k.conns.DownloadTotal), tr("DOWNLOADED"))
			stat(itoa(len(k.rules)), tr("RULES"))
		})
	})
}
