package main

import (
	"embed"
	"log"
	"sync"

	"github.com/egoist/mygo/ui"
)

// The icons are Lucide's (ISC license), as published in lucide-static.
//
//go:embed icons/*.svg
var iconFiles embed.FS

var (
	iconMu    sync.Mutex
	iconCache = map[string]*ui.SVG{}
)

// glyph returns the icon called name, parsed once.
func glyph(name string) *ui.SVG {
	iconMu.Lock()
	defer iconMu.Unlock()
	if svg, ok := iconCache[name]; ok {
		return svg
	}
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		log.Panicf("missing icon %q", name)
	}
	svg := ui.MustParseSVG(data)
	iconCache[name] = svg
	return svg
}
