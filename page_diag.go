package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// diagButton opens the diagnostics, in Home's header.
func (k *Kumo) diagButton(c *ui.Context, s skin) {
	b := ui.ButtonBase(c).Height(32).Padding(0, 12).Gap(6).Radius(16).AlignItems(ui.Center).TextColor(s.text).
		Label(tr("Diagnostics")).Tooltip(tr("Check the core, the system proxy, DNS, the nodes and the network, step by step"))
	if b.Hovered() {
		b.Background(s.hover)
	}
	b.Children(func() {
		ui.Icon(c, glyph("stethoscope")).FontSize(16)
		ui.Text(c, tr("Diagnostics")).FontSize(13).FontWeight(500).SingleLine()
	})
	if b.Clicked() {
		k.openDiagnostics()
	}
}

// diagSheet shows the checks as they run, with their fixes.
func (k *Kumo) diagSheet(c *ui.Context, s skin) {
	d := &k.diag
	if !d.open {
		return
	}
	ui.Modal(c, &d.open, func() {
		ui.Column(c).Width(620).Gap(12).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, glyph("stethoscope")).FontSize(22).TextColor(s.accent)
				ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, tr("Diagnostics")).FontSize(18).Bold().SingleLine()
					sub := tr("Checking…")
					if !d.running {
						sub = diagSummary(d.steps)
					}
					ui.Text(c, sub).FontSize(12.5).TextColor(s.muted).SingleLine()
				})
				if roundButton(c.Key("close"), s, "x", tr("Close")).Clicked() {
					d.open = false
				}
			})
			ui.Scroll(c).MaxHeight(440).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					for i, st := range d.steps {
						k.diagRow(c.Key(st.key), s, i, st)
					}
				})
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, tr("Copy results")).Tooltip(tr("Copy the results as text, for support")).Clicked() {
					_ = mygo.Clipboard.Write(transfer.TextData(k.diagText()))
					k.say(tr("Copied"))
				}
				if d.running {
					ui.Button(c, tr("Checking…"))
				} else if ui.PrimaryButton(c, tr("Run again")).Clicked() {
					k.runDiagnostics()
				}
			})
		})
	})
}

func (k *Kumo) diagRow(c *ui.Context, s skin, i int, st diagStep) {
	row := ui.Row(c).Padding(8, 10).Gap(10).Radius(8).AlignItems(ui.Start)
	if i%2 == 1 {
		row.Background(s.hover)
	}
	row.Children(func() {
		ui.Box(c).Size(18, 18).Center().Margin(1, 0, 0, 0).Children(func() {
			switch st.status {
			case diagRunning:
				ui.Spinner(c).Size(14, 14)
			case diagOK:
				ui.Icon(c, glyph("check")).FontSize(16).TextColor(fast)
			case diagWarn:
				ui.Icon(c, glyph("triangle-alert")).FontSize(15).TextColor(slow)
			case diagFail:
				ui.Icon(c, glyph("x")).FontSize(16).TextColor(dead)
			case diagSkip:
				ui.Icon(c, glyph("minus")).FontSize(15).TextColor(s.muted)
			default:
				light(c, s.muted.Alpha(0.4))
			}
		})
		ui.Column(c).Gap(2).Grow(1).Basis(0).MinWidth(0).Children(func() {
			ui.Text(c, st.title).FontSize(13.5).FontWeight(500)
			if st.detail != "" {
				ui.Text(c, st.detail).FontSize(12).TextColor(s.muted).LineHeight(1.4).Selectable()
			}
		})
		if st.fix != fixNone && (st.status == diagWarn || st.status == diagFail) {
			if ui.Button(c, fixLabel(st.fix)).Clicked() {
				k.diagFix(st)
			}
		}
	})
}

// diagSummary sums up a finished run.
func diagSummary(steps []diagStep) string {
	var warn, fail int
	for _, st := range steps {
		switch st.status {
		case diagWarn:
			warn++
		case diagFail:
			fail++
		}
	}
	switch {
	case fail > 0:
		return itoa(fail) + " " + tr("problems found") + ", " + itoa(warn) + " " + tr("warnings")
	case warn > 0:
		return itoa(warn) + " " + tr("warnings") + "; " + tr("nothing is broken")
	}
	return tr("Everything looks right")
}
