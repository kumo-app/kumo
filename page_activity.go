package main

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"kumo/clash"
)

func matches(q string, fields ...string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return true
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// searchBox is the filter field of a page's header.
func searchBox(c *ui.Context, query *string) ui.Element {
	return ui.TextInput(c, query).Placeholder(tr("Search")).Label(tr("Search")).Width(220)
}

// column is a table column: its title and width, 0 for the rest.
type column struct {
	title string
	width float32
}

func sized(col column, e ui.Element) {
	if col.width == 0 {
		e.Grow(1).Basis(0).MinWidth(80)
	} else {
		e.Width(col.width).Shrink(0)
	}
}

// tableHead is the captions over a table's rows.
func tableHead(c *ui.Context, s skin, cols []column) {
	ui.Row(c).Height(32).Padding(0, 16).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(s.line).Children(func() {
		for i, col := range cols {
			sized(col, label(c.Key(i), s, strings.ToUpper(col.title)).PaddingX(8))
		}
	})
}

// stripe is a table row, every other one washed.
func stripe(c *ui.Context, s skin, index int, height float32) ui.Element {
	r := ui.Row(c).Height(height).Padding(0, 16).AlignItems(ui.Center).FontSize(12.5).FontFeatures("tnum")
	if index%2 == 1 {
		r.Background(s.hover)
	}
	return r
}

func (k *Kumo) rulesPage(c *ui.Context, s skin) {
	k.header(c, s, tr("Rules"), func() {
		ui.Text(c, itoa(len(k.rules))).FontSize(13).TextColor(s.muted)
		ui.Spacer(c)
		searchBox(c, &k.ruleQuery)
	})
	if k.waiting(c, s) {
		return
	}
	var shown []clash.Rule
	var at []int
	for i, r := range k.rules {
		if matches(k.ruleQuery, r.Type, r.Payload, r.Proxy) {
			shown = append(shown, r)
			at = append(at, i)
		}
	}
	cols := []column{{"#", 56}, {tr("Type"), 170}, {tr("Payload"), 0}, {tr("Policy"), 180}}
	tableHead(c, s, cols)
	k.ruleList.Key = func(i int) any { return at[i] }
	ui.List(c.Key("rules"), &k.ruleList, len(shown)).Grow(1).Label(tr("Rules")).Rows(func(row ui.ListRow) {
		rc, r := row.Context, shown[row.Index]
		texts := []string{itoa(at[row.Index] + 1), r.Type, r.Payload, r.Proxy}
		stripe(rc, s, row.Index, 32).Children(func() {
			for i, t := range texts {
				sized(cols[i], ui.Text(rc.Key(i), t).SingleLine().PaddingX(8))
			}
		})
	})
}

func (k *Kumo) connsPage(c *ui.Context, s skin) {
	k.header(c, s, tr("Connections"), func() {
		ui.Text(c, itoa(len(k.conns.Connections))).FontSize(13).TextColor(s.muted)
		ui.Spacer(c)
		searchBox(c, &k.connQuery)
		if ui.Button(c, tr("Close all")).Clicked() {
			k.closeConnection("")
		}
	})
	if k.waiting(c, s) {
		return
	}
	var shown []clash.Connection
	for _, cn := range k.conns.Connections {
		if matches(k.connQuery, cn.Target(), cn.Rule, cn.Payload, strings.Join(cn.Chains, " "), cn.Metadata.Process, cn.Metadata.SourceIP) {
			shown = append(shown, cn)
		}
	}
	if len(shown) == 0 {
		blank(c, s, "cable", tr("No connections"), tr("Connections through the core show here as they open."))
		return
	}
	slices.SortStableFunc(shown, func(a, b clash.Connection) int { return b.Start.Compare(a.Start) })
	cols := []column{{tr("Host"), 0}, {tr("Network"), 90}, {tr("Chain"), 160}, {tr("Rule"), 170}, {"↑", 76}, {"↓", 76}, {tr("Time"), 50}, {"", 40}}
	tableHead(c, s, cols)
	now := time.Now()
	k.connList.Key = func(i int) any { return shown[i].ID }
	ui.List(c.Key("conns"), &k.connList, len(shown)).Grow(1).Label(tr("Connections")).Rows(func(row ui.ListRow) {
		rc, cn := row.Context, shown[row.Index]
		chain := slices.Clone(cn.Chains)
		slices.Reverse(chain)
		rule := cn.Rule
		if cn.Payload != "" {
			rule += " " + cn.Payload
		}
		texts := []string{cn.Target(), cn.Metadata.Network + "/" + cn.Metadata.Type, strings.Join(chain, " › "), rule,
			size(cn.Upload), size(cn.Download), elapsed(now.Sub(cn.Start))}
		stripe(rc, s, row.Index, 34).Children(func() {
			for i, t := range texts {
				e := ui.Text(rc.Key(i), t).SingleLine().PaddingX(8)
				if i == 0 {
					e.FontWeight(500)
				} else if i >= 4 {
					e.TextColor(s.muted)
				}
				sized(cols[i], e)
			}
			ui.Row(rc).Width(40).Shrink(0).Justify(ui.Center).Children(func() {
				if roundButton(rc, s, "x", tr("Close")).Clicked() {
					k.closeConnection(cn.ID)
				}
			})
		})
	})
}

var logLevels = []string{"debug", "info", "warning", "error", "silent"}

func logColor(s skin, kind string) ui.Color {
	switch kind {
	case "warning":
		return slow
	case "error":
		return dead
	case "debug":
		return s.muted
	}
	return s.accent
}

func (k *Kumo) logsPage(c *ui.Context, s skin) {
	k.header(c, s, tr("Logs"), func() {
		ui.Spacer(c)
		level := k.logLevelOr()
		if ui.Select(c, &level, logLevels).Label(tr("Level")).Width(120).Changed() {
			k.setLogLevel(level)
		}
		searchBox(c, &k.logQuery)
		pause := tr("Pause")
		if k.logPaused {
			pause = tr("Resume")
		}
		if ui.Button(c, pause).Clicked() {
			k.logPaused = !k.logPaused
		}
		if ui.Button(c, tr("Clear")).Clicked() {
			k.logs = nil
		}
	})
	if k.waiting(c, s) {
		return
	}
	var shown []clash.Log
	for _, l := range slices.Backward(k.logs) {
		if matches(k.logQuery, l.Payload, l.Type) {
			shown = append(shown, l)
		}
	}
	if len(shown) == 0 {
		blank(c, s, "scroll-text", tr("No logs yet"), tr("The core's log streams here at the level chosen."))
		return
	}
	ui.List(c.Key("logs"), &k.logList, len(shown)).Grow(1).Label(tr("Logs")).Rows(func(row ui.ListRow) {
		rc, l := row.Context, shown[row.Index]
		r := ui.Row(rc).Padding(6, 24).Gap(10).AlignItems(ui.Start).FontSize(12.5)
		if row.Index%2 == 1 {
			r.Background(s.hover)
		}
		r.Children(func() {
			col := logColor(s, l.Type)
			ui.Text(rc, strings.ToUpper(l.Type)).FontSize(10.5).FontWeight(700).Width(76).Shrink(0).
				Padding(2, 6).Radius(4).Background(col.Alpha(0.15)).TextColor(col).TextAlign(ui.Center).SingleLine()
			ui.Text(rc, l.Payload).Font("DejaVu Sans Mono, Menlo, Consolas, monospace").FontSize(12).
				Grow(1).Basis(0).MinWidth(0).LineHeight(1.4).Selectable()
		})
	})
}
