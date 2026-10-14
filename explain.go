package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"kumo/core"
	"kumo/tun"
)

// containsAny reports whether s contains one of subs, ignoring case.
func containsAny(s string, subs ...string) bool {
	s = strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// explainProxyError turns a system-proxy failure into advice.
func explainProxyError(err error, port int) string {
	msg := err.Error()
	switch {
	case containsAny(msg, "gsettings") && containsAny(msg, "not found", "no such file"):
		return fmt.Sprintf(tr("Automatic system proxy needs GNOME. Point your system or browser at 127.0.0.1:%d instead."), port)
	case containsAny(msg, "permission", "not permitted", "access is denied", "authoriz", "admin"):
		return tr("Changing the system proxy needs administrator rights.") + " (" + msg + ")"
	}
	return tr("Could not change the system proxy: ") + msg
}

// explainSubError turns a subscription failure into advice.
func explainSubError(err error) string {
	switch {
	case errors.Is(err, core.ErrURIList):
		return tr("The link returned a list of share links (vmess://, ss://, …), not a Clash configuration. Ask your provider for the Clash or mihomo link, or add “&flag=clash” to it.")
	case errors.Is(err, core.ErrNotClash):
		return tr("The link did not return a Clash configuration. Ask your provider for the Clash or mihomo link, or add “&flag=clash” to it.")
	case errors.Is(err, core.ErrBadLink):
		return tr("Enter a link that starts with https://")
	case errors.Is(err, core.ErrTooLarge):
		return tr("The subscription is larger than 32 MB.")
	case errors.Is(err, core.ErrInsecureRedirect):
		return tr("The subscription redirected from https to http; Kumo did not follow it.")
	}
	return tr("Could not download the subscription: ") + err.Error()
}

// explainCoreError turns the reason the core stopped into advice.
func explainCoreError(msg string) string {
	switch {
	case containsAny(msg, "not bundled", "no core"):
		return tr("This build has no core; reinstall Kumo.")
	case containsAny(msg, "address already in use", "bind:"):
		return tr("A port is taken by another program.")
	}
	return tr("The core stopped unexpectedly.")
}

// explainTunError says why Enhanced Mode could not be turned on.
func explainTunError(err error) string {
	switch {
	case errors.Is(err, tun.ErrUntrusted):
		return tr("Enhanced Mode only uses the core Kumo ships, and this one does not match it. Reinstall Kumo.")
	case errors.Is(err, tun.ErrNoACL):
		return tr("Enhanced Mode needs setfacl (the acl package) to keep the core to your account. Install it and try again.")
	case errors.Is(err, tun.ErrDenied):
		return tr("Enhanced Mode needs your authorization; it stays off.")
	case errors.Is(err, tun.ErrNeedsAdmin):
		return tr("Enhanced Mode needs administrator rights: quit Kumo and run it as administrator.")
	case errors.Is(err, tun.ErrNoTool):
		return tr("Enhanced Mode needs pkexec and setcap (polkit and libcap). Install them, or run Kumo as root.")
	}
	return tr("Could not turn on Enhanced Mode: ") + err.Error()
}

// tunPlan says what turning on Enhanced Mode installs, before the system
// asks for a password.
func tunPlan() string {
	switch runtime.GOOS {
	case "darwin":
		return fmt.Sprintf(tr("macOS will ask for an administrator password once. Kumo then installs a helper (a launchd daemon) and a root-owned copy of the mihomo core in “%s”, after checking it against the core Kumo ships. The helper runs the core as root, only while Kumo asks it to, in a folder of its own."), filepath.Dir(tun.Dest()))
	case "linux":
		return fmt.Sprintf(tr("The system will ask for an administrator password once; its dialog names the program /bin/sh. Kumo then copies the mihomo core to “%s”, after checking it against the core Kumo ships, so that only you can run it, with the network rights cap_net_admin and cap_net_bind_service."), tun.Dest())
	}
	return tr("Enhanced Mode needs Kumo to run as administrator.")
}
