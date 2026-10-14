package main

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"kumo/stats"
)

// statsRanges are the Statistics page's spans: days, or months for the
// last.
var statsRanges = []struct {
	days, months int
}{{7, 0}, {30, 0}, {90, 0}, {0, 12}}

// bar is one bar of the statistics chart.
type bar struct {
	label string
	b     stats.Bytes
}

func (k *Kumo) statsPage(c *ui.Context, s skin) {
	k.header(c, s, tr("Statistics"), func() {
		ui.Spacer(c)
		ui.Segmented(c, &k.statsRange, tr("7 days"), tr("30 days"), tr("90 days"), tr("12 months")).Label(tr("Range")).FontSize(12)
		if ui.Button(c, tr("Clear…")).Tooltip(tr("Forget all traffic statistics")).Clicked() {
			k.confirmClearStats()
		}
	})
	if k.stats == nil {
		return
	}
	rg := statsRanges[k.statsRange]
	var bars []bar
	var sum stats.Bucket
	if rg.days > 0 {
		for _, d := range k.stats.Days(rg.days) {
			bars = append(bars, bar{d.Date.Format("1/2"), d.Total})
			sum.Merge(d.Bucket)
		}
	} else {
		for _, m := range k.stats.Months(rg.months) {
			bars = append(bars, bar{m.Date.Format("2006-01"), m.Total})
			sum.Merge(m.Bucket)
		}
	}
	today := k.stats.Days(1)[0].Total
	month := k.stats.Months(1)[0].Total
	sheetScroll(c, 16, func() {
		ui.Column(c).Gap(16).Children(func() {
			ui.Row(c).Gap(16).Children(func() {
				statFigure(c.Key("today"), s, tr("TODAY"), today)
				statFigure(c.Key("month"), s, tr("THIS MONTH"), month)
				statFigure(c.Key("range"), s, tr("IN RANGE"), sum.Total)
			})
			block(c, s).Gap(12).Children(func() {
				ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
					title := tr("DAILY TRAFFIC")
					if rg.months > 0 {
						title = tr("MONTHLY TRAFFIC")
					}
					label(c, s, title).Grow(1)
					legend(c, s, s.up, tr("Upload"))
					legend(c, s, s.down, tr("Download"))
				})
				barChart(c, s, bars)
			})
			if sum.Total.Sum() == 0 {
				blank(c, s, "chart-column", tr("No traffic yet"), tr("Traffic through the core is counted here by day, app, node and group. Statistics stay on this computer."))
				return
			}
			ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
				topList(c.Key("apps"), s, tr("TOP APPS"), stats.Top(sum.Apps, 8), tr("Other or unknown app"))
				topList(c.Key("nodes"), s, tr("TOP NODES"), stats.Top(sum.Nodes, 8), tr("Other"))
			})
			ui.Row(c).Children(func() {
				topList(c.Key("groups"), s, tr("TOP PROXY GROUPS"), stats.Top(sum.Groups, 6), tr("Other"))
			})
			ui.Text(c, tr("Apps are named when the core can tell the process (in Enhanced Mode, or on this computer's own connections). Bytes of connections that close between two updates count under Other.")).
				FontSize(12).TextColor(s.muted).LineHeight(1.4)
		})
	})
}

func statFigure(c *ui.Context, s skin, title string, b stats.Bytes) {
	block(c, s).Grow(1).Basis(0).Gap(4).Children(func() {
		label(c, s, title)
		v, u := sizeParts(b.Sum())
		figure(c, v, u)
		ui.Text(c, "↑ "+size(b.Up)+"   ↓ "+size(b.Down)).FontSize(12).FontFeatures("tnum").TextColor(s.muted).SingleLine()
	})
}

func legend(c *ui.Context, s skin, col ui.Color, text string) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(10, 10).Radius(3).Background(col)
		ui.Text(c, text).FontSize(12).TextColor(s.muted)
	})
}

// barChart draws download over upload per bar, with the peak and the
// first, middle and last labels.
func barChart(c *ui.Context, s skin, bars []bar) {
	var peak int64 = 1
	for _, b := range bars {
		peak = max(peak, b.b.Sum())
	}
	ui.Box(c).Height(180).Draw(func(p *ui.Painter, r ui.Rect) {
		const foot = 20
		h := r.H - foot - 14
		top := r.Y + 14
		p.Line(r.X, top+h, r.X+r.W, top+h, 1, s.line)
		p.Line(r.X, top, r.X+r.W, top, 1, s.line.Alpha(0.5))
		p.RichText(r.X, r.Y-2, 0, ui.Span{Text: size(peak), Size: 10.5, Color: s.muted})
		n := float32(len(bars))
		slot := r.W / n
		w := max(min(slot*0.62, 28), 2)
		for i, b := range bars {
			x := r.X + slot*float32(i) + (slot-w)/2
			dn := h * float32(b.b.Down) / float32(peak)
			up := h * float32(b.b.Up) / float32(peak)
			if dn > 0 {
				p.Fill(ui.Rect{X: x, Y: top + h - dn, W: w, H: dn}, s.down, min(3, w/2))
			}
			if up > 0 {
				p.Fill(ui.Rect{X: x, Y: top + h - dn - up, W: w, H: up}, s.up, min(3, w/2))
			}
			if i == 0 || i == len(bars)-1 || i == len(bars)/2 {
				lw, _ := p.MeasureText(0, ui.Span{Text: b.label, Size: 10.5})
				lx := min(max(x+w/2-lw/2, r.X), r.X+r.W-lw)
				p.RichText(lx, top+h+5, 0, ui.Span{Text: b.label, Size: 10.5, Color: s.muted})
			}
		}
	})
}

// topList is a ranked table with a bar of each share.
func topList(c *ui.Context, s skin, title string, list []stats.Entry, other string) {
	block(c, s).Grow(1).Basis(0).Gap(6).Children(func() {
		label(c, s, title).Margin(0, 0, 6, 0)
		if len(list) == 0 {
			ui.Text(c, tr("Nothing yet")).FontSize(13).TextColor(s.muted)
			return
		}
		top := max(list[0].Sum(), 1)
		for i, e := range list {
			name := e.Name
			if name == stats.Other {
				name = other
			}
			ui.Column(c.Key(i)).Gap(4).Padding(4, 0).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, name).FontSize(13).FontWeight(500).SingleLine().Grow(1).MinWidth(0).Tooltip(name)
					ui.Text(c, size(e.Sum())).FontSize(12.5).FontFeatures("tnum").TextColor(s.muted).SingleLine()
				})
				share := float32(e.Sum()) / float32(top)
				ui.Box(c).Height(4).Radius(2).Background(s.hover).Draw(func(p *ui.Painter, r ui.Rect) {
					p.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(r.W*share, 3), H: r.H}, s.accent.Alpha(0.75), 2)
				})
			})
		}
	})
}

func (k *Kumo) confirmClearStats() {
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: k.win, Type: mygo.MessageWarning,
			Message: tr("Clear traffic statistics?"),
			Detail:  tr("Every day and month counted so far is forgotten. This cannot be undone."),
			Buttons: []string{tr("Clear"), tr("Cancel")}, DefaultButton: 1, CancelButton: 1})
		if err != nil || res.Button != 0 {
			return
		}
		err = k.stats.Clear()
		k.post(func() {
			if err != nil {
				k.say(fmt.Sprint(tr("The statistics could not be cleared: "), err))
				return
			}
			k.say(tr("Traffic statistics cleared"))
		})
	}()
}
