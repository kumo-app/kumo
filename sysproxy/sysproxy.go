// Package sysproxy sets the system's proxy to the core's port and puts the
// user's own settings back: networksetup on macOS, the WinINet settings on
// Windows, and GNOME's settings (gsettings) on Linux.
package sysproxy

import (
	"encoding/json"
	"os"
)

// Bypass are the hosts that go around the proxy.
var Bypass = []string{"localhost", "127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "*.local"}

// State is the system's proxy settings as they were before Kumo set them,
// in the terms of the system: keys and values Restore puts back.
type State struct {
	Values map[string]string `json:"values"`
}

// Snapshot reads the system's proxy settings.
func Snapshot() (State, error) { return snapshot() }

// Restore puts settings read by Snapshot back.
func Restore(s State) error { return restore(s) }

// Set points the system's HTTP, HTTPS and SOCKS proxies at host:port.
func Set(host string, port int) error { return set(host, port) }

// ClearOurs turns off the system's proxies that point at host:port, and
// leaves any other proxy of the user alone. Kumo uses it when it has no
// backup to restore.
func ClearOurs(host string, port int) error { return clearOurs(host, port) }

// SaveBackup keeps the state at path until the proxy is put back, so that
// a crash of Kumo can be repaired at its next launch.
func SaveBackup(path string, s State) error {
	data, _ := json.Marshal(s)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadBackup reads a backup, false when there is none.
func LoadBackup(path string) (State, bool) {
	var s State
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &s) != nil {
		return s, false
	}
	return s, true
}
