package tun

import "golang.org/x/sys/windows"

// Dest is unused on Windows: the core runs elevated with Kumo.
func Dest() string { return "" }

func core(src string) (Launch, bool) {
	if windows.GetCurrentProcessToken().IsElevated() {
		return Launch{Binary: src}, true
	}
	return Launch{}, false
}

func install(string) error { return ErrNeedsAdmin }

func legacy() bool { return false }

func removeLegacy() error { return nil }

// LegacyPath is unused on Windows.
func LegacyPath() string { return "" }
