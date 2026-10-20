package main

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"

	"kumo/clash"
	"kumo/core"
)

func (k *Kumo) nodesPage(c *ui.Context, s skin) {
	k.header(c, s, tr("Nodes"), func() {
		ui.Spacer(c)
		order := 0
		if k.prefs.UI.SortByLatency {
			order = 1
		}
		if ui.Segmented(c, &order, tr("Default order"), tr("By latency")).Label(tr("Sort")).FontSize(12).Changed() {
			k.prefs.UI.SortByLatency = order == 1
			_ = k.prefs.save()
		}
		text := tr("Test all")
		if len(k.testing) > 0 {
			text = tr("Testing…")
		}
		if ui.PrimaryButton(c, text).Tooltip(tr("Measure the latency of every node")).Clicked() && len(k.testing) == 0 {
			var all []string
			for n, p := range k.proxies {
				if !p.IsGroup() && p.Type != "Reject" && p.Type != "RejectDrop" {
					all = append(all, n)
				}
			}
			k.measure(all, nil)
		}
	})
	if k.waiting(c, s) {
		return
	}
	if len(k.groups) == 0 {
		blank(c, s, "globe", tr("No nodes yet"), tr("Add a subscription on the Subscriptions page; its nodes show here."))
		return
	}
	cols := 3
	if w, _ := c.Size(); w-k.sideShown() > 1100 {
		cols = 4
	}
	k.nodeSheet(c, s)
	k.nodeRows = k.nodeRowsOf(k.nodeRows[:0], cols)
	rows := k.nodeRows
	k.nodeList.Key = func(i int) any { return rows[i].key }
	// One list of the groups' headers and rows of cards: it lays out only
	// the rows in view, where a page of every card took 20 ms a frame with
	// 300 nodes. Each row is a slice of its group's block.
	ui.List(c.Key("nodes"), &k.nodeList, len(rows)).Grow(1).Label(tr("Nodes")).Rows(func(row ui.ListRow) {
		r := rows[row.Index]
		k.nodeRow(row.Context, s, r, cols, row.Index == len(rows)-1)
	})
}

// nodeRow is one row of the Nodes list: a group's header, or a row of its
// cards; the first and the last round the group's block.
type nodeRow struct {
	key         string
	group       string
	names       []string // the cards; none for the header
	first, last bool
}

// nodeRowsOf lays the groups out as rows of cols cards, reusing out.
func (k *Kumo) nodeRowsOf(out []nodeRow, cols int) []nodeRow {
	for _, name := range k.groups {
		if name == "GLOBAL" && k.cfg.Mode != "global" {
			continue
		}
		names := k.groupNames(k.proxies[name])
		out = append(out, nodeRow{key: "h\x00" + name, group: name, first: true, last: len(names) == 0})
		for i := 0; i < len(names); i += cols {
			end := min(i+cols, len(names))
			out = append(out, nodeRow{key: "r\x00" + name + "\x00" + itoa(i/cols), group: name, names: names[i:end], last: end == len(names)})
		}
	}
	return out
}

// groupNames are the nodes of g as its cards show them.
func (k *Kumo) groupNames(g clash.Proxy) []string {
	names := slices.DeleteFunc(slices.Clone(g.All), func(n string) bool { return n == core.TestGroup })
	if k.prefs.UI.SortByLatency {
		slices.SortStableFunc(names, func(a, b string) int { return rank(k.proxies[a]) - rank(k.proxies[b]) })
	}
	return names
}

func (k *Kumo) nodeRow(c *ui.Context, s skin, r nodeRow, cols int, end bool) {
	g := k.proxies[r.group]
	var above, below, top, bottom, round, roundEnd float32 = 0, 0, 0, 8, 0, 0
	if r.first {
		above, top, bottom, round = 16, 16, 12, 18
	}
	if r.last {
		bottom, roundEnd = 16, 18
	}
	if end {
		below = 24
	}
	ui.Column(c).Padding(above, 24, below, 24).Children(func() {
		seg := ui.Column(c).Background(s.tile).Padding(top, 18, bottom, 18).
			Radius(round, round, roundEnd, roundEnd).Shadow(0, 1, 2, 0, s.shadow)
		if r.last {
			seg.Shadow(0, 10, 28, -12, s.shadow)
		}
		seg.Children(func() {
			if r.names == nil {
				k.groupHead(c, s, g)
				return
			}
			manual := g.Type == "Selector"
			ui.Grid(c).Columns(cols).Gap(8).Children(func() {
				for _, n := range r.names {
					k.nodeCard(c.Key(n), s, g, n, manual)
				}
			})
		})
	})
}

// kindLabel describes how a group chooses.
func kindLabel(t string) string {
	switch t {
	case "Selector":
		return tr("Manual")
	case "URLTest":
		return tr("Auto: fastest")
	case "Fallback":
		return tr("Auto: fallback")
	case "LoadBalance":
		return tr("Load balance")
	}
	return t
}

// groupHead is a group's header: its name, how it chooses, the node in
// use and its tests.
func (k *Kumo) groupHead(c *ui.Context, s skin, g clash.Proxy) {
	manual := g.Type == "Selector"
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, g.Name).FontSize(16).FontWeight(600).SingleLine()
		hint := tr("This group picks a node by itself")
		if manual {
			hint = tr("Click a node to use it")
		}
		tag(c, s, kindLabel(g.Type)).Tooltip(hint)
		ui.Icon(c, glyph("chevron-right")).FontSize(14).TextColor(s.muted)
		ui.Text(c, k.resolve(g.Name)).FontSize(13.5).FontWeight(500).TextColor(s.accent).SingleLine().Grow(1).MinWidth(0)
		busy := false
		for _, n := range g.All {
			busy = busy || k.testing[n]
		}
		if busy {
			ui.Spinner(c).Size(16, 16)
		}
		if ui.Button(c, tr("Test speed")).Tooltip(tr("Measure the latency of this group's nodes")).Clicked() && !busy {
			k.measure(g.All, nil)
		}
		if manual && ui.Button(c, tr("Use fastest")).Tooltip(tr("Test, then switch to the node with the lowest latency")).Clicked() && !busy {
			k.pickFastest(g.Name)
		}
	})
}

// rank orders nodes by latency: tested ones first, timeouts last.
func rank(p clash.Proxy) int {
	switch ms := p.LastDelay(); {
	case len(p.History) == 0:
		return 1 << 20
	case ms <= 0:
		return 1 << 21
	default:
		return ms
	}
}

func (k *Kumo) nodeCard(c *ui.Context, s skin, g clash.Proxy, name string, manual bool) {
	p := k.proxies[name]
	chosen := g.Now == name
	b := ui.ButtonBase(c).Column().AlignItems(ui.Stretch).Padding(10, 12).Gap(6).Radius(10).
		BorderWidth(1, 1, 1, 1).BorderColor(s.line).Label(name).Tooltip(name)
	switch {
	case chosen:
		b.Background(s.accent.Alpha(0.16)).BorderColor(s.accent).BorderWidth(2, 2, 2, 2)
	case manual && b.Hovered():
		b.Background(s.hover)
	}
	b.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			weight := 500
			if chosen {
				weight = 650
			}
			ui.Text(c, name).FontSize(13.5).FontWeight(weight).SingleLine().Grow(1).MinWidth(0)
			if chosen {
				ui.Icon(c, glyph("check")).FontSize(14).TextColor(s.accent)
			}
			if testable(p) {
				info := ui.ButtonBase(c.Key("info")).Size(22, 22).Radius(11).Center().Label(tr("Node details")).Tooltip(tr("Latency history, outages and tests"))
				if info.Hovered() {
					info.Background(s.hover)
				}
				info.Children(func() { ui.Icon(c, glyph("activity")).FontSize(13).TextColor(s.muted) })
				if info.Clicked() {
					k.openNode(name)
				}
			}
		})
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			kind := p.Type
			if p.UDP {
				kind += " · UDP"
			}
			ui.Text(c, kind).FontSize(11.5).TextColor(s.muted).SingleLine().Grow(1).MinWidth(0)
			if k.testing[name] {
				ui.Spinner(c).Size(12, 12)
				return
			}
			ms, tested := p.LastDelay(), len(p.History) > 0
			col := latencyColor(s, ms, tested)
			ui.Text(c, latencyText(ms, tested)).FontSize(11).FontWeight(600).FontFeatures("tnum").Padding(1, 6).Radius(4).
				Background(col.Alpha(0.14)).TextColor(col).SingleLine()
		})
		k.cardMonitor(c, s, name)
	})
	if b.Clicked() {
		if manual {
			k.choose(g.Name, name)
		} else {
			k.say(fmt.Sprintf(tr("%s picks its node automatically; switch a manual group to choose."), g.Name))
		}
	}
}
