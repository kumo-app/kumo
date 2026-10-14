package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// compactRate is a speed short enough for the menu bar: "0 B/s",
// "4 KB/s", "12.3 MB/s"; one decimal below ten, none above.
func compactRate(n int64) string {
	if n < 0 {
		n = 0
	}
	units := []string{"B/s", "KB/s", "MB/s", "GB/s"}
	v, u := float64(n), 0
	for v >= 1000 && u < len(units)-1 { // 1000 keeps it at four digits
		v /= 1024
		u++
	}
	if u > 0 && v < 10 {
		return fmt.Sprintf("%.1f %s", v, units[u])
	}
	return fmt.Sprintf("%.0f %s", v, units[u])
}

// speedText is what the tray shows: upload over download on macOS (two
// lines in an image), one line elsewhere; "" when off or offline.
func (k *Kumo) speedText() string {
	if k.prefs.App.HideTraySpeed || !k.online {
		return ""
	}
	return compactRate(k.traffic.Up) + "\n" + compactRate(k.traffic.Down)
}

// syncSpeed updates the tray when the speed text changes (about once a
// second, following /traffic). macOS gets an image: the glyph and two
// right-aligned lines, as a template so the menu bar tints it; Linux a
// label; Windows, whose tray has no title, the tooltip.
func (k *Kumo) syncSpeed() {
	if k.tray == nil {
		return
	}
	text := k.speedText()
	if text == k.speedSig {
		return
	}
	k.speedSig = text
	up, down, _ := cutLine(text)
	switch runtime.GOOS {
	case "darwin":
		if text == "" {
			_ = k.tray.SetIcon(trayGlyph, true)
		} else if img, err := speedIcon(trayGlyph, up, down); err == nil {
			_ = k.tray.SetIcon(img, true)
		}
	case "windows":
		tip := "Kumo"
		if text != "" {
			tip += "\n↑ " + up + "\n↓ " + down
		}
		k.tray.SetToolTip(tip)
	default:
		if text == "" {
			k.tray.SetTitle("")
		} else {
			k.tray.SetTitle("↑" + up + " ↓" + down)
		}
	}
}

func cutLine(s string) (a, b string, ok bool) {
	for i := range len(s) {
		if s[i] == '\n' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// Pixel geometry at 2x: the menu bar shows the image 18 points tall.
const (
	speedH    = 36
	speedSize = 15 // pixels per line of text
	speedGap  = 6  // between the glyph and the text
)

var (
	speedFace     font.Face
	speedFaceErr  error
	speedFaceOnce sync.Once
)

func monoFace() (font.Face, error) {
	speedFaceOnce.Do(func() {
		f, err := opentype.Parse(gomonobold.TTF)
		if err != nil {
			speedFaceErr = err
			return
		}
		speedFace, speedFaceErr = opentype.NewFace(f, &opentype.FaceOptions{Size: speedSize, DPI: 72, Hinting: font.HintingFull})
	})
	return speedFace, speedFaceErr
}

// speedIcon draws the glyph and the two lines in black on transparent.
// The text column is as wide as "999 KB/s" so the item does not jump
// about as the numbers change.
func speedIcon(glyph []byte, up, down string) ([]byte, error) {
	face, err := monoFace()
	if err != nil {
		return nil, err
	}
	g, err := png.Decode(bytes.NewReader(glyph))
	if err != nil {
		return nil, err
	}
	gb := g.Bounds()
	gw := gb.Dx() * speedH / gb.Dy()
	col := font.MeasureString(face, "999 KB/s").Ceil()
	for _, s := range []string{up, down} {
		col = max(col, font.MeasureString(face, s).Ceil())
	}
	img := image.NewNRGBA(image.Rect(0, 0, gw+speedGap+col, speedH))
	scaleInto(img, image.Rect(0, 0, gw, speedH), g)
	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.Black), Face: face}
	m := face.Metrics()
	lineH := (m.Ascent + m.Descent).Ceil()
	top := (speedH - 2*lineH) / 2
	for i, s := range []string{up, down} {
		w := font.MeasureString(face, s)
		d.Dot = fixed.Point26_6{X: fixed.I(img.Bounds().Dx()) - w, Y: fixed.I(top+i*lineH) + m.Ascent}
		d.DrawString(s)
	}
	var buf bytes.Buffer
	err = png.Encode(&buf, img)
	return buf.Bytes(), err
}

// scaleInto copies src into r of dst, nearest neighbour with 2x2
// supersampling for a smooth edge.
func scaleInto(dst *image.NRGBA, r image.Rectangle, src image.Image) {
	sb := src.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			var a uint32
			for _, o := range [][2]int{{1, 1}, {3, 1}, {1, 3}, {3, 3}} {
				sx := sb.Min.X + ((x-r.Min.X)*4+o[0])*sb.Dx()/(r.Dx()*4)
				sy := sb.Min.Y + ((y-r.Min.Y)*4+o[1])*sb.Dy()/(r.Dy()*4)
				_, _, _, aa := src.At(sx, sy).RGBA()
				a += aa
			}
			dst.SetNRGBA(x, y, color.NRGBA{A: uint8(a / 4 >> 8)})
		}
	}
}
