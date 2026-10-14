package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"kumo/monitor"
	"kumo/nodetest"
)

// The node monitor and tests on the Nodes page: a line under each card,
// and a sheet with the whole record.

func (k *Kumo) openNode(name string) {
	k.detail, k.detailOpen = name, true
}

// cardMonitor is a card's monitor line: uptime and a small latency line,
// with the last speed and UDP results. In simple mode only the uptime and
// results show, and nothing before the first test.
func (k *Kumo) cardMonitor(c *ui.Context, s skin, name string) {
	if k.mon == nil {
		return
	}
	sum, ok := k.mon.Summary(name)
	chk := k.mon.Checks(name)
	busy := k.nodeTest.name == name
	if !ok && chk.SpeedAt.IsZero() && chk.UDPAt.IsZero() && !busy {
		return
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Height(16).Children(func() {
		if ok {
			col := uptimeColor(s, sum.Uptime)
			ui.Text(c, uptimeText(sum.Uptime)).FontSize(10.5).FontWeight(600).FontFeatures("tnum").TextColor(col).SingleLine().
				Tooltip(fmt.Sprintf(tr("Up %s of %d tests since Kumo started"), uptimeText(sum.Uptime), sum.Tests))
			if k.prefs.UI.AdvancedMode {
				samples := k.mon.Samples(name)
				ui.Box(c).Grow(1).Height(14).MinWidth(20).Draw(func(p *ui.Painter, r ui.Rect) {
					latencyLine(p, r, samples, 30, s.accent, dead, 1.25)
				})
			} else {
				ui.Spacer(c)
			}
		} else {
			ui.Spacer(c)
		}
		switch {
		case busy:
			ui.Spinner(c).Size(10, 10)
		default:
			if chk.Mbps > 0 {
				ui.Text(c, mbps(chk.Mbps)+" Mbps").FontSize(10.5).FontFeatures("tnum").TextColor(s.muted).SingleLine()
			}
			if chk.UDP == int(nodetest.UDPSupported) {
				ui.Text(c, "UDP ✓").FontSize(10.5).TextColor(fast).SingleLine().Tooltip(tr("UDP supported"))
			} else if chk.UDP == int(nodetest.UDPUnsupported) {
				ui.Text(c, "UDP ✗").FontSize(10.5).TextColor(dead).SingleLine().Tooltip(tr("UDP not supported"))
			}
		}
	})
}

func uptimeText(u float64) string {
	if u >= 0.9995 {
		return "100%"
	}
	return fmt.Sprintf("%.1f%%", u*100)
}

func uptimeColor(s skin, u float64) ui.Color {
	switch {
	case u >= 0.99:
		return fast
	case u >= 0.9:
		return slow
	}
	return dead
}

// latencyLine draws the last n samples (all when n is 0) across r: a line
// through the successes, a tick at the bottom for each failure.
func latencyLine(p *ui.Painter, r ui.Rect, samples []monitor.Sample, n int, stroke, fail ui.Color, width float32) {
	if n > 0 && len(samples) > n {
		samples = samples[len(samples)-n:]
	}
	if len(samples) == 0 {
		return
	}
	peak := 1
	for _, sm := range samples {
		peak = max(peak, sm.MS)
	}
	slots := max(len(samples), n)
	x := func(i int) float32 {
		if slots <= 1 {
			return r.X + r.W
		}
		return r.X + r.W*float32(i+slots-len(samples))/float32(slots-1)
	}
	var line ui.Path
	drawing := false
	for i, sm := range samples {
		if !sm.OK() {
			p.Fill(ui.Rect{X: x(i) - 1, Y: r.Y + r.H - 4, W: 2, H: 4}, fail, 1)
			drawing = false
			continue
		}
		y := r.Y + r.H - 1 - (r.H-2)*float32(sm.MS)/float32(peak)
		if !drawing {
			line.MoveTo(x(i), y)
			drawing = true
		} else {
			line.LineTo(x(i), y)
		}
	}
	p.StrokePath(&line, width, stroke)
}

// nodeSheet is the node's whole record and its tests.
func (k *Kumo) nodeSheet(c *ui.Context, s skin) {
	if k.detail == "" {
		return
	}
	name := k.detail
	ui.Modal(c, &k.detailOpen, func() {
		p := k.proxies[name]
		sum, ok := k.mon.Summary(name)
		chk := k.mon.Checks(name)
		samples := k.mon.Samples(name)
		outages := k.mon.Outages(name)
		ui.Column(c).Width(640).Gap(14).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).FontSize(18).Bold().SingleLine().Grow(1).MinWidth(0)
				kind := p.Type
				if p.UDP {
					kind += " · UDP"
				}
				tag(c, s, kind)
				if roundButton(c.Key("close"), s, "x", tr("Close")).Clicked() {
					k.detailOpen = false
				}
			})
			ui.Text(c, tr("Since Kumo started; nothing is saved.")).FontSize(12).TextColor(s.muted)
			ui.Row(c).Gap(10).Children(func() {
				up, avg, p95, jit, seen := "–", "–", "–", "–", "–"
				if ok {
					up = uptimeText(sum.Uptime)
					if sum.Avg > 0 {
						avg, p95, jit = itoa(sum.Avg)+" ms", itoa(sum.P95)+" ms", itoa(sum.Jitter)+" ms"
					}
					if !sum.LastSeen.IsZero() {
						seen = ago(time.Since(sum.LastSeen))
					}
				}
				for i, f := range [][2]string{{tr("UPTIME"), up}, {tr("AVERAGE"), avg}, {"P95", p95}, {tr("JITTER"), jit}, {tr("LAST UP"), seen}} {
					ui.Column(c.Key(i)).Grow(1).Basis(0).Gap(3).Padding(10, 12).Radius(10).Background(s.hover).Children(func() {
						label(c, s, f[0])
						ui.Text(c, f[1]).FontSize(16).FontWeight(600).FontFeatures("tnum").SingleLine()
					})
				}
			})
			ui.Column(c).Gap(6).Children(func() {
				ui.Row(c).Children(func() {
					label(c, s, tr("LATENCY")).Grow(1)
					if ok {
						ui.Text(c, fmt.Sprintf(tr("%d tests · %d failed"), sum.Tests, sum.Failures)).FontSize(11.5).TextColor(s.muted)
					}
				})
				ui.Box(c).Height(110).Radius(10).Background(s.hover).Draw(func(pt *ui.Painter, r ui.Rect) {
					if len(samples) == 0 {
						return
					}
					inner := ui.Rect{X: r.X + 10, Y: r.Y + 18, W: r.W - 20, H: r.H - 28}
					latencyLine(pt, inner, samples, 0, s.accent, dead, 1.5)
					pt.RichText(r.X+10, r.Y+3, 0, ui.Span{Text: samples[0].At.Format("15:04"), Size: 10.5, Color: s.muted})
					last := samples[len(samples)-1].At.Format("15:04")
					w, _ := pt.MeasureText(0, ui.Span{Text: last, Size: 10.5})
					pt.RichText(r.X+r.W-10-w, r.Y+3, 0, ui.Span{Text: last, Size: 10.5, Color: s.muted})
				})
				if !ok {
					ui.Text(c, tr("No tests yet. The node monitor tests every node in the background; set how often in Settings.")).FontSize(12).TextColor(s.muted)
				}
			})
			ui.Column(c).Gap(4).Children(func() {
				label(c, s, tr("OUTAGES")).Margin(0, 0, 4, 0)
				if len(outages) == 0 {
					ui.Text(c, tr("None")).FontSize(12.5).TextColor(s.muted)
				}
				now := time.Now()
				for i, o := range outages[:min(len(outages), 6)] {
					end := tr("ongoing")
					if !o.End.IsZero() {
						end = o.End.Format("15:04:05")
					}
					stripe(c.Key(i), s, i, 26).Padding(0, 8).Gap(10).Children(func() {
						ui.Text(c, o.Start.Format("15:04:05")+" – "+end).Grow(1).SingleLine()
						ui.Text(c, ago(o.Duration(now))).TextColor(s.muted).SingleLine()
						ui.Text(c, fmt.Sprintf(tr("%d failed"), o.Failures)).TextColor(dead).SingleLine()
					})
				}
				if len(outages) > 6 {
					ui.Text(c, fmt.Sprintf(tr("and %d earlier"), len(outages)-6)).FontSize(12).TextColor(s.muted)
				}
			})
			ui.Column(c).Gap(8).Children(func() {
				label(c, s, tr("TESTS"))
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					busy := k.nodeTest.name != ""
					if ui.Button(c, tr("Latency")).Clicked() && !k.testing[name] {
						k.measure([]string{name}, nil)
					}
					if k.nodeTest.name == name && k.nodeTest.kind == "speed" {
						if ui.Button(c, fmt.Sprintf(tr("Cancel · %s Mbps"), mbps(k.nodeTest.progress.Mbps()))).Clicked() {
							k.cancelNodeTest()
						}
					} else if ui.Button(c, tr("Download speed")).Tooltip(tr("Downloads through this node for a few seconds; uses data")).Clicked() && !busy {
						k.testNode(name, "speed")
					}
					if ui.Button(c, tr("UDP")).Tooltip(tr("Sends a DNS query over UDP through this node")).Clicked() && !busy {
						k.testNode(name, "udp")
					}
					if busy && k.nodeTest.name == name {
						ui.Spinner(c).Size(14, 14)
					}
				})
				ui.Row(c).Gap(16).Children(func() {
					speed := tr("Speed: not tested")
					switch {
					case chk.SpeedErr != "":
						speed = tr("Speed: failed")
					case chk.Mbps > 0:
						speed = fmt.Sprintf(tr("Speed: %s Mbps (%s)"), mbps(chk.Mbps), when(chk.SpeedAt))
					}
					ui.Text(c, speed).FontSize(12.5).TextColor(s.muted).Tooltip(chk.SpeedErr)
					udp := tr("UDP: not tested")
					if !chk.UDPAt.IsZero() {
						udp = udpText(chk.UDP) + " (" + when(chk.UDPAt) + ")"
					}
					ui.Text(c, udp).FontSize(12.5).TextColor(s.muted)
				})
			})
		})
	})
}

// ago is a duration the short way: 45s, 12m, 3h 5m.
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}
