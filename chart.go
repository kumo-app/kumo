package main

import "github.com/egoist/mygo/ui"

// sparkline draws the last historyLen samples across r as a line over a
// flat fill, the newest at the right edge, zero padded on the left. The
// lowest 36 points of r stay below the line so the tile's text clears it.
func sparkline(p *ui.Painter, r ui.Rect, samples series, stroke, fill ui.Color) {
	const floor = 36
	var peak int64 = 1
	for _, v := range samples {
		peak = max(peak, v)
	}
	pad := historyLen - len(samples)
	point := func(i int) (float32, float32) {
		var v int64
		if j := i - pad; j >= 0 {
			v = samples[j]
		}
		return r.X + r.W*float32(i)/float32(historyLen-1),
			r.Y + r.H - floor - (r.H-floor-2)*float32(v)/float32(peak)
	}
	var area ui.Path
	var line [historyLen][2]float32
	area.MoveTo(r.X, r.Y+r.H)
	for i := range historyLen {
		x, y := point(i)
		area.LineTo(x, y)
		line[i] = [2]float32{x, y}
	}
	area.LineTo(r.X+r.W, r.Y+r.H)
	area.Close()
	p.FillPath(&area, fill)
	// A live line: on a GPU StrokePolyline draws its segments directly
	// rather than rasterizing a mask of the whole path every second. The
	// chart has one point per second, fewer than its pixels, so it needs
	// no bucketing (chart.MinMax).
	p.StrokePolyline(line[:], 2, stroke)
}
