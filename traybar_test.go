package main

import (
	"bytes"
	"image/png"
	"os"
	"testing"
)

func TestCompactRate(t *testing.T) {
	for n, want := range map[int64]string{
		-5: "0 B/s", 0: "0 B/s", 999: "999 B/s", 1000: "1.0 KB/s", 4096: "4.0 KB/s",
		12 * 1024: "12 KB/s", 999 * 1024: "999 KB/s", 1500 * 1024: "1.5 MB/s", 3 << 30: "3.0 GB/s",
	} {
		if got := compactRate(n); got != want {
			t.Errorf("compactRate(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestSpeedIcon(t *testing.T) {
	data, err := speedIcon(trayGlyph, "4.0 KB/s", "1.2 MB/s")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dy() != speedH || b.Dx() <= speedH {
		t.Fatalf("bounds %v", b)
	}
	small, _ := speedIcon(trayGlyph, "0 B/s", "0 B/s")
	si, _ := png.Decode(bytes.NewReader(small))
	if si.Bounds() != img.Bounds() {
		t.Fatalf("width must not change with the numbers: %v vs %v", si.Bounds(), img.Bounds())
	}
	if p := os.Getenv("KUMO_SPEED_PNG"); p != "" {
		_ = os.WriteFile(p, data, 0o644)
	}
}

func TestSpeedTextOffWhenHidden(t *testing.T) {
	k := &Kumo{prefs: &Prefs{}, online: true}
	k.traffic.Up, k.traffic.Down = 4096, 1024
	if got := k.speedText(); got != "4.0 KB/s\n1.0 KB/s" {
		t.Fatalf("got %q", got)
	}
	k.prefs.App.HideTraySpeed = true
	if k.speedText() != "" {
		t.Fatal("hidden speed still shown")
	}
}
