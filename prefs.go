package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"kumo/core"
)

// Prefs are Kumo's preferences, stored as JSON in the data directory.
// The core's own options live in the core and change over its API.
type Prefs struct {
	Core struct {
		// External connects to a controller Kumo does not run.
		External bool `json:"external"`
		// Port and Secret are the bundled core's controller.
		Port   int    `json:"port"`
		Secret string `json:"secret"`
		// MixedPort is opened when the profile opens no port.
		MixedPort       int  `json:"mixedPort"`
		AutoSystemProxy bool `json:"autoSystemProxy"`
		Tun             bool `json:"tun"`
		AllowLan        bool `json:"allowLan"`
	} `json:"core"`
	Controller struct {
		Host   string `json:"host"`
		Port   int    `json:"port"`
		Secret string `json:"secret"`
		// TLS speaks https and wss to the controller.
		TLS         bool   `json:"tls"`
		TestURL     string `json:"testUrl"`
		TestTimeout int    `json:"testTimeout"`
	} `json:"controller"`
	App struct {
		// Theme is "system", "light" or "dark".
		Theme string `json:"theme"`
		// SidebarColor tints the window: one of sidebarHues.
		SidebarColor string `json:"sidebarColor"`
		Language     string `json:"language"`
		// HideTraySpeed turns off the speed beside the tray icon (on by default).
		HideTraySpeed bool `json:"hideTraySpeed"`
		// NoUpdateCheck turns off the daily check for a new Kumo.
		NoUpdateCheck bool `json:"noUpdateCheck"`
		// UpdateChannel is "stable" or "beta".
		UpdateChannel string `json:"updateChannel"`
		// SkipVersion is a version the user chose to skip.
		SkipVersion string `json:"skipVersion,omitempty"`
		// LastUpdateCheck is when Kumo last looked (Unix seconds).
		LastUpdateCheck int64 `json:"lastUpdateCheck,omitempty"`
	} `json:"app"`
	UI struct {
		AdvancedMode  bool `json:"advancedMode"`
		SortByLatency bool `json:"sortByLatency"`
		SidebarHidden bool `json:"sidebarHidden"`
	} `json:"ui"`

	path      string
	envSecret string
	loadErr   string // why the saved settings could not be read
}

// loadPrefs reads path, filling in defaults for what it lacks.
func loadPrefs(path string) *Prefs {
	p := &Prefs{path: path}
	p.Core.Port = 9097
	p.Core.MixedPort = 7890
	p.Controller.Host = "127.0.0.1"
	p.Controller.Port = 9090
	p.Controller.TestURL = "https://www.gstatic.com/generate_204"
	p.Controller.TestTimeout = 5000
	p.App.Theme = "system"
	p.App.SidebarColor = "blue"
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, p); err != nil {
			kept, _ := core.KeepCorrupt(path)
			p.loadErr = fmt.Sprintf("%v (%s)", err, kept)
		}
	}
	// A secret from the environment is used, never saved.
	p.envSecret = os.Getenv("KUMO_SECRET")
	return p
}

// save writes the preferences in one step.
func (p *Prefs) save() error {
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return core.WriteFileAtomic(p.path, data, 0o600)
}
