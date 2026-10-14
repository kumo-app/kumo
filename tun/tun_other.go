//go:build !linux && !darwin && !windows

package tun

func Dest() string { return "" }

func core(string) (Launch, bool) { return Launch{}, false }

func install(string) error { return ErrNoTool }

func legacy() bool { return false }

func removeLegacy() error { return nil }

func LegacyPath() string { return "" }
