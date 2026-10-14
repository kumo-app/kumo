package sysproxy

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func gsettings(args ...string) (string, error) {
	out, err := exec.Command("gsettings", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gsettings %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// keys are the settings Kumo changes, as schema and key.
var keys = [][2]string{
	{"org.gnome.system.proxy", "mode"}, {"org.gnome.system.proxy", "ignore-hosts"},
	{"org.gnome.system.proxy.http", "host"}, {"org.gnome.system.proxy.http", "port"},
	{"org.gnome.system.proxy.https", "host"}, {"org.gnome.system.proxy.https", "port"},
	{"org.gnome.system.proxy.socks", "host"}, {"org.gnome.system.proxy.socks", "port"},
}

func snapshot() (State, error) {
	s := State{Values: map[string]string{}}
	for _, k := range keys {
		v, err := gsettings("get", k[0], k[1])
		if err != nil {
			return s, err
		}
		s.Values[k[0]+" "+k[1]] = v
	}
	return s, nil
}

func restore(s State) error {
	// The mode last, once the hosts it points at are back.
	for i := len(keys) - 1; i >= 0; i-- {
		k := keys[i]
		if v, ok := s.Values[k[0]+" "+k[1]]; ok {
			if _, err := gsettings("set", k[0], k[1], v); err != nil {
				return err
			}
		}
	}
	return nil
}

func set(host string, port int) error {
	p := strconv.Itoa(port)
	ignore := "['" + strings.Join(Bypass, "', '") + "']"
	for _, args := range [][]string{
		{"set", "org.gnome.system.proxy.http", "host", host},
		{"set", "org.gnome.system.proxy.http", "port", p},
		{"set", "org.gnome.system.proxy.https", "host", host},
		{"set", "org.gnome.system.proxy.https", "port", p},
		{"set", "org.gnome.system.proxy.socks", "host", host},
		{"set", "org.gnome.system.proxy.socks", "port", p},
		{"set", "org.gnome.system.proxy", "ignore-hosts", ignore},
		{"set", "org.gnome.system.proxy", "mode", "manual"},
	} {
		if _, err := gsettings(args...); err != nil {
			return err
		}
	}
	return nil
}

func clearOurs(host string, port int) error {
	mode, err := gsettings("get", "org.gnome.system.proxy", "mode")
	if err != nil || strings.Trim(mode, "'") != "manual" {
		return err
	}
	h, _ := gsettings("get", "org.gnome.system.proxy.http", "host")
	p, _ := gsettings("get", "org.gnome.system.proxy.http", "port")
	if strings.Trim(h, "'") != host || p != strconv.Itoa(port) {
		return nil // the user's own proxy
	}
	_, err = gsettings("set", "org.gnome.system.proxy", "mode", "none")
	return err
}
