package sysproxy

import (
	"fmt"
	"os/exec"
	"strings"
)

func runNetworksetup(args ...string) (string, error) {
	out, err := exec.Command("networksetup", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("networksetup %s: %v %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

var mac = macProxy{ns: runNetworksetup}

func snapshot() (State, error)              { return mac.snapshot() }
func restore(s State) error                 { return mac.restore(s) }
func set(host string, port int) error       { return mac.set(host, port) }
func clearOurs(host string, port int) error { return mac.clearOurs(host, port) }
