#!/bin/sh
# Takes the README screenshots into docs/screenshots/ at 2x: the packaged
# app (build/linux-amd64, with its bundled core) runs on its own Xvfb
# display with GDK_SCALE=2 in a WIDTHxHEIGHT window, while
# scripts/traffic.sh drives traffic through it. Each line of SHOTS is
# "name page data-dir theme"; data-dir is a KUMO_DATA directory set up
# beforehand. Needs Xvfb and Pillow.
set -e
cd "$(dirname "$0")/.."
OUT=${OUT:-docs/screenshots}
mkdir -p "$OUT"
BIN=$(realpath "${BIN:-build/linux-amd64/kumo}")
SIZE=${SIZE:-1280x760}
Xvfb :55 -screen 0 3000x2000x24 >/dev/null 2>&1 &
xvfb=$!
trap 'kill $xvfb 2>/dev/null; pkill -f scripts/traffic.sh; pkill -f "$BIN"' EXIT
sleep 2
echo "${SHOTS:?set SHOTS}" | while read -r name page data theme; do
	[ -n "$name" ] || continue
	pkill -f "$BIN" || true
	sleep 2
	rm -f "$data/window-state.json"
	python3 - "$data/settings.json" "$theme" <<'PY'
import json, sys
p, theme = sys.argv[1], sys.argv[2]
try: s = json.load(open(p))
except Exception: s = {}
s.setdefault("app", {})["theme"] = theme
json.dump(s, open(p, "w"), indent=2)
PY
	DISPLAY=:55 GDK_SCALE=2 KUMO_WINDOW=$SIZE KUMO_DATA=$data KUMO_PAGE=/$page "$BIN" >/dev/null 2>&1 &
	sleep 6
	sh scripts/traffic.sh >/dev/null 2>&1 &
	sleep "${WAIT:-40}"
	pkill -f scripts/traffic.sh || true
	python3 -c "
from PIL import ImageGrab
im = ImageGrab.grab(xdisplay=':55')
im.crop(im.getbbox()).save('$OUT/$name.png')"
done
