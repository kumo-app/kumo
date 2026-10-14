//go:build unix

package tun

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// helperPaths are the files of the macOS helper for one user:
//
//	<root>/mihomo             the core, root's, checked against the pins
//	<root>/helper-<uid>.sh    the daemon's program, root's
//	<root>/home/<uid>/        the core's home (-d), root's, 0700
//	<root>/logs/<uid>/        root's: core.log and state, readable by the user
//	<root>/users/<uid>/       the user's, 0700: request and config.yaml
type helperPaths struct {
	root string
	uid  int
}

func (p helperPaths) core() string   { return filepath.Join(p.root, "mihomo") }
func (p helperPaths) filter() string { return filepath.Join(p.root, "kumo-filter") }

func (p helperPaths) helper() string {
	return filepath.Join(p.root, "helper-"+strconv.Itoa(p.uid)+".sh")
}
func (p helperPaths) home() string    { return filepath.Join(p.root, "home", strconv.Itoa(p.uid)) }
func (p helperPaths) logs() string    { return filepath.Join(p.root, "logs", strconv.Itoa(p.uid)) }
func (p helperPaths) users() string   { return filepath.Join(p.root, "users", strconv.Itoa(p.uid)) }
func (p helperPaths) request() string { return filepath.Join(p.users(), "request") }
func (p helperPaths) config() string  { return filepath.Join(p.users(), "config.yaml") }
func (p helperPaths) state() string   { return filepath.Join(p.logs(), "state") }
func (p helperPaths) log() string     { return filepath.Join(p.logs(), "core.log") }
func (p helperPaths) label() string   { return "app.kumo.tun." + strconv.Itoa(p.uid) }

// helperScript is the daemon's program. launchd starts it, as root, when
// the user's request file changes. A request is
//
//	start <client pid> <nonce> <controller port>
//
// and anything else means stop. The script copies the configuration,
// reading it as the user, into the root-owned home, runs the core there
// with the controller forced onto 127.0.0.1, and stops it when the request
// changes or the client is gone. The core's files stay in that home, so
// nothing the user owns is written as root, and mihomo keeps every path a
// configuration names inside it.
//
// The core's controller is only a unix socket in that home. Kumo reaches
// it through the filter (RunFilter: Kumo's own executable, copied into the
// root-owned directory) on 127.0.0.1:<port>, which takes the configuration's
// secret, given to it in its environment, and refuses requests that could
// load a configuration opening listeners. With cdhash set, the client must
// be the Kumo build that installed the helper, checked by its code
// signature as the kernel sees it; afterwards the process ID must stay alive
// and named Kumo.
func helperScript(p helperPaths, cdhash string) string {
	return fmt.Sprintf(`#!/bin/sh
# Kumo Enhanced Mode helper (%[1]s). Installed by Kumo; runs as root.
PATH=/usr/bin:/bin:/usr/sbin:/sbin
U=%[2]d
R=%[3]s
Q=%[4]s
H=%[5]s
L=%[6]s
readreq() { [ -f "$Q/request" ] && [ ! -L "$Q/request" ] && head -c 200 "$Q/request"; }
say() { echo "$1" > "$L/state"; }
# Kumo writes plain block YAML without these keys; refuse anything else at
# the top level, so a configuration cannot open listeners to the network
# or hide keys behind quotes, flow style, merge keys, anchors or tags.
# Then the filter (Kumo) parses it and allows no listener but Kumo's test
# listener on loopback (core.CheckRuntime).
safe() { ! grep -Eq '^([^[:space:]#a-z]|(tunnels|redir-port|tproxy-port|ss-config|vmess-config|tuic-server|shadowsocks-config|external-controller-tls|external-controller-pipe|external-controller-unix|external-doh-server|external-ui|external-ui-url)[[:space:]]*:)' "$1"; }
C=%[7]s
signed() { [ -z "$C" ] || codesign --verify -R="cdhash H\"$C\"" "$1" 2>/dev/null; }
client() { kill -0 "$1" 2>/dev/null && ps -p "$1" -o comm= | grep -qi kumo; }
while :; do
	req=$(readreq) || exit 0
	set -- $req
	[ "$1" = start ] && [ $# -eq 4 ] || exit 0
	pid=$2 nonce=$3 port=$4
	case "$pid$port$nonce" in *[!0-9a-f]*) exit 0;; esac
	client "$pid" && signed "$pid" || { say "stopped $nonce 4"; exit 0; }
	if ! sudo -n -u "#$U" /bin/cat "$Q/config.yaml" > "$H/config.yaml.new"; then
		say "stopped $nonce 2"; exit 0
	fi
	if ! safe "$H/config.yaml.new" || ! env -i KUMO_CONFIG_CHECK="$H/config.yaml.new" "$R/kumo-filter" > /dev/null 2>&1; then
		echo "Kumo helper: the configuration opens listeners or hides keys; refused." > "$L/core.log"
		say "stopped $nonce 3"; exit 0
	fi
	sec=$(sed -n 's/^secret:[[:space:]]*//p' "$H/config.yaml.new" | head -n 1 | tr -d "\"'\r")
	sed -E '/^(secret|external-controller)[[:space:]]*:/d' "$H/config.yaml.new" > "$H/config.yaml"
	rm -f "$H/config.yaml.new" "$H/ctl.sock"
	env -i PATH=/usr/bin:/bin:/usr/sbin:/sbin HOME="$H" "$R/mihomo" -d "$H" -f "$H/config.yaml" \
		-ext-ctl-unix "$H/ctl.sock" > "$L/core.log" 2>&1 &
	core=$!
	env -i KUMO_CTL_FILTER="$H/ctl.sock" KUMO_FILTER_LISTEN="127.0.0.1:$port" KUMO_FILTER_SECRET="$sec" \
		"$R/kumo-filter" > "$L/filter.log" 2>&1 &
	filter=$!
	say "running $nonce"
	while kill -0 $core 2>/dev/null && kill -0 $filter 2>/dev/null; do
		[ "$(readreq)" = "$req" ] && client "$pid" || break
		sleep 0.3
	done
	kill -TERM $filter 2>/dev/null
	if kill -0 $core 2>/dev/null; then
		kill -INT $core
		i=0
		while kill -0 $core 2>/dev/null && [ $i -lt 30 ]; do sleep 0.1; i=$((i+1)); done
		kill -KILL $core 2>/dev/null
	fi
	wait $core
	say "stopped $nonce $?"
	[ "$(readreq)" = "$req" ] && exit 0
done
`, p.label(), p.uid, shellQuote(p.root), shellQuote(p.users()), shellQuote(p.home()), shellQuote(p.logs()), shellQuote(cdhash))
}

// plist is the launchd daemon of the helper.
func plist(p helperPaths) string {
	esc := func(s string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + esc(p.label()) + `</string>
	<key>ProgramArguments</key>
	<array><string>/bin/sh</string><string>` + esc(p.helper()) + `</string></array>
	<key>WatchPaths</key>
	<array><string>` + esc(p.request()) + `</string></array>
	<key>RunAtLoad</key><false/>
</dict>
</plist>
`
}

// parseCDHash finds the CDHash in codesign -dvvv's output.
func parseCDHash(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(line), "CDHash="); ok {
			for _, c := range h {
				if !strings.ContainsRune("0123456789abcdef", c) {
					return ""
				}
			}
			return h
		}
	}
	return ""
}
