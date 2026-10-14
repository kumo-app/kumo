package main

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
)

// Kumo's look: a window tinted by a chosen hue with a vertical gradient,
// the sidebar floating on it, white tiles on the home screen and a white
// sheet under every other page.

// sidebarHues are the window tints offered in Settings.
var sidebarHues = []struct {
	id   string
	hue  float32
	gray bool
}{
	{"blue", 215, false}, {"violet", 264, false}, {"pink", 334, false},
	{"orange", 25, false}, {"gold", 45, false}, {"green", 145, false},
	{"cyan", 186, false}, {"gray", 220, true},
}

// swatchOf is the color a tint shows as in Settings.
func swatchOf(hue float32, gray bool) ui.Color {
	if gray {
		return hueColor(hue, 0.05, 0.62)
	}
	return hueColor(hue, 0.78, 0.6)
}

// skin holds the colors of the current frame beyond mygo's theme.
type skin struct {
	top, bottom    ui.Color // the window's gradient
	sheet, tile    ui.Color
	text, muted    ui.Color
	line           ui.Color
	hover, pick    ui.Color
	accent         ui.Color
	up, upFill     ui.Color
	down, downFill ui.Color
	shadow         ui.Color
	good           ui.Color
	isDark         bool
}

// dress sets the frame's theme and returns its skin.
func (k *Kumo) dress(c *ui.Context) skin {
	t := *c.Theme()
	hue, gray := sidebarHues[0].hue, false
	for _, h := range sidebarHues {
		if h.id == k.prefs.App.SidebarColor {
			hue, gray = h.hue, h.gray
		}
	}
	sat := float32(0.58)
	if gray {
		sat = 0.04
	}
	var s skin
	if t.Dark {
		s = skin{
			isDark: true,
			top:    hueColor(hue+40, sat*0.72, 0.22), bottom: hueColor(hue, sat*0.72, 0.17),
			sheet: ui.RGBA(17, 17, 26, 0.86), tile: ui.RGBA(24, 24, 36, 0.82),
			text: ui.RGB(250, 250, 250), muted: ui.RGB(161, 161, 170),
			line:  ui.RGBA(255, 255, 255, 0.1),
			hover: ui.RGBA(255, 255, 255, 0.06), pick: ui.RGBA(255, 255, 255, 0.14),
			accent: ui.Hex("#3b82f6"),
			down:   hueColor(193, 0.83, 0.55), up: hueColor(243, 0.92, 0.72),
			shadow: ui.RGBA(0, 0, 0, 0.35), good: ui.Hex("#22c55e"),
		}
		s.downFill, s.upFill = s.down.Alpha(0.18), s.up.Alpha(0.18)
	} else {
		s = skin{
			top: hueColor(hue, sat, 0.96), bottom: hueColor(hue, sat, 0.9),
			sheet: ui.RGBA(255, 255, 255, 0.92), tile: ui.RGB(255, 255, 255),
			text: ui.RGB(10, 10, 10), muted: ui.RGB(115, 115, 115),
			line:  ui.RGBA(0, 0, 0, 0.09),
			hover: ui.RGBA(0, 0, 0, 0.04), pick: ui.RGBA(0, 0, 0, 0.09),
			accent: ui.Hex("#2f7df6"),
			down:   hueColor(193, 0.83, 0.5), downFill: ui.Hex("#e6f8fc"),
			up: hueColor(243, 0.96, 0.66), upFill: ui.Hex("#f1f0ff"),
			shadow: ui.RGBA(15, 23, 42, 0.08), good: ui.Hex("#10b981"),
		}
	}
	black := ui.RGB(0, 0, 0)
	t.Accent, t.AccentHover, t.AccentPressed = s.accent, s.accent.Mix(black, 0.08), s.accent.Mix(black, 0.16)
	t.AccentText = ui.RGB(255, 255, 255)
	t.Text, t.TextMuted, t.Border = s.text, s.muted, s.line
	t.Radius = 8
	c.SetTheme(&t)
	return s
}

// hueColor is a color from hue (degrees), saturation and lightness (0–1).
func hueColor(h, sat, light float32) ui.Color {
	h = float32(math.Mod(float64(h), 360))
	if h < 0 {
		h += 360
	}
	chroma := (1 - float32(math.Abs(float64(2*light-1)))) * sat
	second := chroma * (1 - float32(math.Abs(math.Mod(float64(h)/60, 2)-1)))
	base := light - chroma/2
	var rgb [3]float32
	switch sector := int(h / 60); sector {
	case 0:
		rgb = [3]float32{chroma, second, 0}
	case 1:
		rgb = [3]float32{second, chroma, 0}
	case 2:
		rgb = [3]float32{0, chroma, second}
	case 3:
		rgb = [3]float32{0, second, chroma}
	case 4:
		rgb = [3]float32{second, 0, chroma}
	default:
		rgb = [3]float32{chroma, 0, second}
	}
	ch := func(v float32) uint8 { return uint8(math.Round(float64((v + base) * 255))) }
	return ui.RGB(ch(rgb[0]), ch(rgb[1]), ch(rgb[2]))
}

// tile is a card of the home screen.
func tile(c *ui.Context, s skin) ui.Element {
	return ui.Column(c).MinHeight(162).Padding(16).Radius(18).Background(s.tile).Clip().
		Shadow(0, 1, 2, 0, s.shadow).Shadow(0, 10, 28, -12, s.shadow)
}

// block is a white card on a page's sheet, as a node group.
func block(c *ui.Context, s skin) ui.Element {
	return ui.Column(c).Padding(20).Radius(18).Background(s.tile).Clip().
		Shadow(0, 1, 2, 0, s.shadow).Shadow(0, 10, 28, -12, s.shadow)
}

// label is the small spaced caption heading a tile or a column.
func label(c *ui.Context, s skin, text string) ui.Element {
	return ui.Text(c, text).FontSize(11).FontWeight(600).LetterSpacing(0.8).TextColor(s.muted).SingleLine()
}

// figure is a tile's big number with its unit.
func figure(c *ui.Context, value, unit string) {
	ui.Row(c).Gap(5).AlignItems(ui.End).Margin(6, 0, 0, 0).Children(func() {
		ui.Text(c, value).FontSize(34).Bold().LetterSpacing(-0.6).FontFeatures("tnum").FixedLineHeight(38).SingleLine()
		if unit != "" {
			ui.Text(c, unit).FontSize(12.5).FontWeight(500).Margin(0, 0, 5, 0).SingleLine()
		}
	})
}

// light is a small round status lamp.
func light(c *ui.Context, color ui.Color) ui.Element {
	return ui.Box(c).Size(8, 8).Radius(4).Background(color)
}

// tag is a small gray label, as a group's kind.
func tag(c *ui.Context, s skin, text string) ui.Element {
	return ui.Text(c, text).FontSize(11).Padding(2, 6).Radius(4).Background(s.hover).TextColor(s.muted).SingleLine()
}

// roundButton is a round icon-only button.
func roundButton(c *ui.Context, s skin, icon, name string) ui.Element {
	b := ui.ButtonBase(c).Size(32, 32).Radius(16).Center().Label(name).Tooltip(name).TextColor(s.text)
	if b.Hovered() {
		b.Background(s.hover)
	}
	return b.Children(func() { ui.Icon(c, glyph(icon)).FontSize(17) })
}

// blank is a page's empty state: an icon, a title and a line.
func blank(c *ui.Context, s skin, icon, title, detail string) {
	ui.Column(c).Grow(1).Center().Gap(10).Padding(32).Children(func() {
		if icon != "" {
			ui.Icon(c, glyph(icon)).FontSize(34).TextColor(s.muted).Margin(0, 0, 6, 0)
		}
		ui.Text(c, title).FontSize(19).FontWeight(600).TextAlign(ui.Center)
		ui.Text(c, detail).FontSize(14.5).TextColor(s.muted).TextAlign(ui.Center).MaxWidth(420).LineHeight(1.45)
	})
}

var (
	fast = ui.Hex("#22c55e")
	slow = ui.Hex("#f59e0b")
	dead = ui.Hex("#ef4444")
)

// latencyColor grades a latency: green under 200 ms, amber under 500,
// red above or on timeout, muted when untested.
func latencyColor(s skin, ms int, tested bool) ui.Color {
	switch {
	case !tested:
		return s.muted
	case ms <= 0:
		return dead
	case ms < 200:
		return fast
	case ms < 500:
		return slow
	}
	return dead
}

func latencyText(ms int, tested bool) string {
	switch {
	case !tested:
		return tr("Untested")
	case ms <= 0:
		return tr("Timeout")
	}
	return strconv.Itoa(ms) + " ms"
}

func itoa(n int) string { return strconv.Itoa(n) }

// sizeParts splits a byte count into a number and a unit.
func sizeParts(n int64) (string, string) {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	switch {
	case i == 0:
		return strconv.FormatInt(n, 10), units[0]
	case v >= 100:
		return strconv.FormatFloat(math.Round(v), 'f', 0, 64), units[i]
	case v >= 10:
		return strconv.FormatFloat(v, 'f', 1, 64), units[i]
	}
	return strconv.FormatFloat(v, 'f', 2, 64), units[i]
}

func size(n int64) string {
	v, u := sizeParts(n)
	return v + " " + u
}

// when is a short time: the clock today, the date otherwise.
func when(t time.Time) string {
	now := time.Now()
	switch {
	case t.IsZero():
		return "–"
	case t.YearDay() == now.YearDay() && t.Year() == now.Year():
		return t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	}
	return t.Format("2006-01-02")
}

// elapsed is a connection's age, in its largest unit.
func elapsed(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
