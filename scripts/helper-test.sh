#!/bin/sh
# End-to-end check of the macOS Enhanced Mode helper protocol, on Linux
# with sudo: the helper script runs as root (as launchd would run it),
# Kumo's executable runs as its client, and a real mihomo core runs as
# root in the helper's home. TUN itself stays off.
#
#   sh scripts/helper-test.sh build/linux-amd64/kumo resources/linux-amd64/core/mihomo
set -eu
KUMO=$(realpath "$1"); CORE=$(realpath "$2")
U=$(id -u); G=$(id -g)
R=$(mktemp -d /tmp/kumo-helper.XXXXXX)
W=$(mktemp -d /tmp/kumo-helper-user.XXXXXX)
cleanup() { sudo -n pkill -f "$R/helper" 2>/dev/null || true; sudo -n rm -rf "$R"; rm -rf "$W"; }
trap cleanup EXIT
KUMO_HELPER_ROOT="$R" KUMO_HELPER_OUT="$W/helper.sh" go test -count=1 -run TestWriteHelper ./tun >/dev/null
sudo -n sh -c "
  chown root:root '$R' && chmod 0755 '$R'
  install -d -o root -g root -m 0755 '$R/home' '$R/logs' '$R/users'
  install -d -o root -g root -m 0700 '$R/home/$U'
  install -d -o root -g root -m 0755 '$R/logs/$U'
  install -d -o $U -g $G -m 0700 '$R/users/$U'
  for f in core.log state; do : > '$R/logs/$U/'\$f; chown $U '$R/logs/$U/'\$f; chmod 0600 '$R/logs/$U/'\$f; done
  install -o root -g root -m 0755 '$CORE' '$R/mihomo'
  install -o root -g root -m 0755 '$W/helper.sh' '$R/helper-$U.sh'
  install -o root -g root -m 0755 '$KUMO' '$R/kumo-filter'
"
PORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])')
MIX=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])')
mkdir -p "$W/core"
printf 'mixed-port: %s\nexternal-controller: 127.0.0.1:%s\nsecret: s3cret\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n' "$MIX" "$PORT" > "$W/core/runtime.yaml"

# Kumo's client, in the core's place.
KUMO_TUN_CLIENT="$R" "$KUMO" -d "$W/core" -f "$W/core/runtime.yaml" > "$W/client.log" 2>&1 &
C=$!
sleep 0.5
# launchd would start the helper when the request appears.
sudo -n sh "$R/helper-$U.sh" &
for i in $(seq 50); do curl -s -H 'Authorization: Bearer s3cret' "http://127.0.0.1:$PORT/version" >/dev/null && break; sleep 0.2; done
V=$(curl -s --max-time 2 -H 'Authorization: Bearer s3cret' "http://127.0.0.1:$PORT/version") || { echo "FAIL: no controller"; cat "$W/client.log"; sudo -n cat "$R/logs/$U/state" "$R/logs/$U/core.log"; ls -la "$W/core" "$R/users/$U"; exit 1; }
echo "controller: $V"
OWNER=$(ps -o user= -p "$(pgrep -f "$R/mihomo" | head -1)")
echo "core runs as: $OWNER"
[ "$OWNER" = root ] || { echo "FAIL: core not root"; exit 1; }
sudo -n ls "$R/home/$U" | tr '\n' ' '; echo "(home: root-owned)"
[ -z "$(find "$W/core" -user 0)" ] || { echo "FAIL: root files in the user's dir"; exit 1; }
echo "PASS: the helper runs the core as root in its own home; nothing root-owned in the user's folder"

# The controller goes through the filter: secret required, configs that
# open listeners refused, Kumo's own reload and mode changes allowed.
A='Authorization: Bearer s3cret'
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
[ "$(code "http://127.0.0.1:$PORT/version")" = 401 ] || { echo "FAIL: no secret accepted"; exit 1; }
[ "$(code -X PUT -H "$A" -d '{"payload":"mixed-port: '$MIX'\nlisteners: [{name: x, type: mixed, port: 7999, listen: 0.0.0.0}]\n"}' "http://127.0.0.1:$PORT/configs")" = 403 ] || { echo "FAIL: listener config loaded"; exit 1; }
[ "$(code -X PUT -H "$A" -d '{"path":"/etc/passwd"}' "http://127.0.0.1:$PORT/configs")" = 403 ] || { echo "FAIL: path load"; exit 1; }
[ "$(code -X PATCH -H "$A" -d '{"redir-port":7998}' "http://127.0.0.1:$PORT/configs")" = 403 ] || { echo "FAIL: redir patch"; exit 1; }
[ "$(code -X PATCH -H "$A" -d '{"mode":"global"}' "http://127.0.0.1:$PORT/configs")" = 204 ] || { echo "FAIL: mode patch"; exit 1; }
[ "$(code -X PUT -H "$A" -d '{"payload":"mixed-port: '$MIX'\nmode: rule\nproxies: []\nrules:\n  - MATCH,DIRECT\n"}' "http://127.0.0.1:$PORT/configs?force=true")" = 204 ] || { echo "FAIL: Kumo reload"; exit 1; }
ss -ltn | grep -q ':7999 ' && { echo "FAIL: listener opened"; exit 1; }
curl -s -m 2 -N -H "$A" "http://127.0.0.1:$PORT/traffic" | head -c 40 | grep -q up || { echo "FAIL: streaming through the filter"; exit 1; }
sudo -n ss -ltnp | grep ":$PORT " | grep -q kumo-filter || { echo "FAIL: the controller port is not the filter's"; sudo -n ss -ltnp | grep ":$PORT "; exit 1; }
echo "PASS: the controller is the filter: secret, listener configs, path loads and patches refused; Kumo's calls pass"

# A configuration that opens a listener to the network is refused.
kill -INT $C; wait $C || true
sleep 1
pgrep -f "$R/mihomo" >/dev/null && { echo "FAIL: core still running after the client stopped"; exit 1; }
pgrep -f "$R/kumo-filter" >/dev/null && { echo "FAIL: filter still running"; exit 1; }
echo "PASS: stopping the client stops the root core"

printf 'listeners: [{name: x, type: mixed, port: 7999, listen: 0.0.0.0}]\n' >> "$W/core/runtime.yaml"
KUMO_TUN_CLIENT="$R" "$KUMO" -d "$W/core" -f "$W/core/runtime.yaml" > "$W/client2.log" 2>&1 &
C=$!
sleep 0.5
sudo -n sh "$R/helper-$U.sh" || true
wait $C && { echo "FAIL: client succeeded"; exit 1; } || true
grep -q refused "$W/client2.log" && echo "PASS: a configuration with listeners is refused" || { cat "$W/client2.log"; exit 1; }

# Kumo dying (client killed hard) stops the root core too.
sed -i '$d' "$W/core/runtime.yaml"
KUMO_TUN_CLIENT="$R" "$KUMO" -d "$W/core" -f "$W/core/runtime.yaml" > "$W/client3.log" 2>&1 &
C=$!
sleep 0.5
sudo -n sh "$R/helper-$U.sh" &
for i in $(seq 50); do pgrep -f "$R/mihomo" >/dev/null && break; sleep 0.2; done
kill -9 $C
sleep 2
pgrep -f "$R/mihomo" >/dev/null && { echo "FAIL: core outlived its client"; exit 1; }
echo "PASS: the root core ends when its client dies"
