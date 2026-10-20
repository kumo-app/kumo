package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"kumo/clash"
)

// openRoute opens the route sheet for a connection, or for the test
// alone with id "".
func (k *Kumo) openRoute(id string) {
	k.routeConn, k.routeOpen = id, true
}

// routeChain draws a route as the group header draws a group's choice:
// the rule, then each hop after a chevron.
func routeChain(c *ui.Context, s skin, r route) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
		if m := r.matched(); m != "" {
			tag(c, s, m).Tooltip(tr("The rule that matched"))
		}
		for i, n := range r.chain {
			ui.Icon(c.Key(i), glyph("chevron-right")).FontSize(13).TextColor(s.muted)
			t := ui.Text(c.Key(n), n).FontSize(13).FontWeight(500).SingleLine()
			if i == len(r.chain)-1 {
				t.TextColor(s.accent).FontWeight(600)
			}
		}
	})
}

func (k *Kumo) routeSheet(c *ui.Context, s skin) {
	if !k.routeOpen {
		return
	}
	var cn clash.Connection
	known := false
	if k.routeConn != "" {
		for _, x := range k.conns.Connections {
			if x.ID == k.routeConn {
				cn, known = x, true
				break
			}
		}
	}
	ui.Modal(c, &k.routeOpen, func() {
		ui.Column(c).Width(600).Gap(14).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, glyph("route")).FontSize(20).TextColor(s.accent)
				title := tr("Why this route")
				if known {
					title = cn.Target()
				}
				ui.Text(c, title).FontSize(18).Bold().SingleLine().Grow(1).MinWidth(0).Selectable()
				if roundButton(c.Key("close"), s, "x", tr("Close")).Clicked() {
					k.routeOpen = false
				}
			})
			if k.routeConn != "" {
				if !known {
					ui.Text(c, tr("This connection has closed.")).FontSize(13).TextColor(s.muted)
				} else {
					k.connFacts(c, s, cn)
					r := routeOf(cn)
					ui.Column(c).Gap(8).Padding(12, 14).Radius(10).Background(s.hover).Children(func() {
						label(c, s, tr("WHY THIS ROUTE"))
						routeChain(c, s, r)
						ui.Text(c, r.explain(k.cfg.Mode)).FontSize(13).LineHeight(1.45).Selectable()
					})
				}
				ui.Divider(c).Background(s.line)
			}
			k.routeTester(c, s)
		})
	})
}

// connFacts are a connection's particulars, as the node sheet's figures.
func (k *Kumo) connFacts(c *ui.Context, s skin, cn clash.Connection) {
	src := cn.Metadata.SourceIP
	if cn.Metadata.SourcePort != "" {
		src += ":" + cn.Metadata.SourcePort
	}
	proc := cn.Metadata.Process
	if proc == "" {
		proc = "–"
	}
	ui.Row(c).Gap(10).Children(func() {
		for i, f := range [][2]string{{tr("APP"), proc}, {tr("FROM"), src}, {tr("NETWORK"), cn.Metadata.Network + "/" + cn.Metadata.Type}, {tr("TRAFFIC"), "↑ " + size(cn.Upload) + "  ↓ " + size(cn.Download)}} {
			ui.Column(c.Key(i)).Grow(1).Basis(0).MinWidth(0).Gap(3).Padding(10, 12).Radius(10).Background(s.hover).Children(func() {
				label(c, s, f[0])
				ui.Text(c, f[1]).FontSize(13.5).FontWeight(600).FontFeatures("tnum").SingleLine().Selectable()
			})
		}
	})
}

// routeTester is the "Test a host" box.
func (k *Kumo) routeTester(c *ui.Context, s skin) {
	t := &k.routeTest
	ui.Column(c).Gap(8).Children(func() {
		label(c, s, tr("TEST A HOST"))
		ui.Text(c, tr("Kumo makes a request to it through its own proxy and shows the rule and the chain the core chose, then closes the connection.")).
			FontSize(12).TextColor(s.muted).LineHeight(1.4)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c, &t.input).Label(tr("Host to test")).Placeholder(tr("example.com or https://…")).Grow(1).MinWidth(0).
				OnSubmit(func() {
					if !t.running {
						k.runRouteTest()
					}
				})
			if t.running {
				ui.Spinner(c).Size(16, 16)
			}
			if ui.PrimaryButton(c, tr("Test")).Clicked() && !t.running {
				k.runRouteTest()
			}
		})
		if !t.done {
			return
		}
		ui.Column(c).Gap(8).Padding(12, 14).Radius(10).Background(s.hover).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, hostOf(t.target)).FontSize(13.5).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
				switch {
				case t.status > 0:
					ui.Text(c, fmt.Sprintf("HTTP %d · %d ms", t.status, t.elapsed.Milliseconds())).FontSize(12).TextColor(fast).FontFeatures("tnum").SingleLine()
				case t.err != "":
					ui.Text(c, tr("Failed")).FontSize(12).TextColor(dead).SingleLine()
				}
			})
			if t.found {
				routeChain(c, s, t.route)
				ui.Text(c, t.route.explain(k.cfg.Mode)).FontSize(13).LineHeight(1.45).Selectable()
				if t.from == "log" {
					ui.Text(c, tr("Read from the core's log: the request ended before the core listed it.")).FontSize(11.5).TextColor(s.muted)
				}
			} else if t.target != "" {
				ui.Text(c, tr("The core did not list the request, so its route is unknown. Set the log level to info and test again.")).FontSize(12.5).TextColor(slow).LineHeight(1.4)
			}
			if t.err != "" {
				ui.Text(c, strings.TrimSpace(t.err)).FontSize(12).TextColor(s.muted).MaxLines(3).Selectable()
			}
		})
	})
}
