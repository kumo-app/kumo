#!/bin/sh
# Checks, on Linux under an X display, that
#  1. one quit request (SIGTERM, which mygo handles exactly like ⌘Q and the
#     Quit menu items) ends Kumo while closing the window would only hide
#     it, stops the core and puts back the user's own proxy settings;
#  2. a launch after a crash repairs the proxy settings Kumo left behind.
# gsettings is replaced by a stub keeping its values in files.
# Usage: scripts/quit-test.sh [binary]
set -u
BIN=$(realpath "${1:-build/linux-amd64/kumo}")
DATA=$(mktemp -d)
STUB=$(mktemp -d)
mkdir "$STUB/db"
cat > "$STUB/gsettings" <<STUBEOF
#!/bin/sh
f="$STUB/db/\$2.\$3"
case "\$1" in
get) if [ -f "\$f" ]; then cat "\$f"; else echo "''"; fi ;;
set) printf '%s\n' "\$4" > "\$f"; echo "\$2 \$3 \$4" >> "$STUB/log" ;;
esac
STUBEOF
chmod +x "$STUB/gsettings"
mode() { cat "$STUB/db/org.gnome.system.proxy.mode" 2>/dev/null; }
echo "'auto'" > "$STUB/db/org.gnome.system.proxy.mode"   # the user's own setting
fail() { echo "FAIL: $*"; [ -n "${pid:-}" ] && kill -9 "$pid" 2>/dev/null; cat "$DATA/app.log"; exit 1; }
wait_for() { # seconds, test
	n=$1; shift
	for i in $(seq 1 $((n * 2))); do eval "$@" && return 0; sleep 0.5; done
	return 1
}

printf '{"core":{"autoSystemProxy":true},"app":{"language":"en"}}' > "$DATA/settings.json"
PATH="$STUB:$PATH" KUMO_DATA="$DATA" KUMO_HIDE_ON_CLOSE=1 "$BIN" >"$DATA/app.log" 2>&1 &
pid=$!
wait_for 45 '[ "$(mode)" = manual ]' || fail "system proxy not set (mode $(mode))"
core=$(pgrep -f "$DATA/core" | head -1)
[ -n "$core" ] || fail "the core did not start"
[ -f "$DATA/sysproxy-backup.json" ] || fail "no backup of the user's settings"
kill -TERM $pid
wait_for 5 '! kill -0 $pid 2>/dev/null' || fail "Kumo still running 5s after a quit request"
kill -0 "$core" 2>/dev/null && fail "core still running"
[ "$(mode)" = "'auto'" ] || fail "user's proxy mode not restored (mode $(mode))"
echo "PASS: quit ends Kumo, stops the core and restores the system proxy"

# A crash: Kumo's proxy is on and the backup still there.
PATH="$STUB:$PATH" KUMO_DATA="$DATA" "$BIN" >"$DATA/app.log" 2>&1 &
pid=$!
wait_for 45 '[ "$(mode)" = manual ]' || fail "system proxy not set again"
kill -9 $pid
pkill -9 -f "$DATA/core"
printf '{"core":{"autoSystemProxy":false},"app":{"language":"en"}}' > "$DATA/settings.json"
PATH="$STUB:$PATH" KUMO_DATA="$DATA" "$BIN" >"$DATA/app.log" 2>&1 &
pid=$!
wait_for 30 '[ "$(mode)" = "'"'auto'"'" ]' || fail "stale proxy not repaired at launch (mode $(mode))"
wait_for 5 '[ ! -f "$DATA/sysproxy-backup.json" ]' || fail "backup left behind"
kill -TERM $pid
wait_for 5 '! kill -0 $pid 2>/dev/null' || fail "Kumo still running"
echo "PASS: a launch after a crash restores the user's proxy settings"
