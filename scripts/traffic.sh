#!/bin/sh
# Drives steady traffic through Kumo's mixed port, for the screenshots'
# charts: a rate-limited download plus a few small requests, forever.
PROXY=${PROXY:-http://127.0.0.1:7890}
while :; do
	curl -s -o /dev/null -x "$PROXY" --limit-rate "${RATE:-1600k}" "https://speed.cloudflare.com/__down?bytes=40000000" &
	curl -s -o /dev/null -x "$PROXY" --limit-rate 300k -m 25 "https://speed.cloudflare.com/__down?bytes=9000000" &
	curl -s -o /dev/null -x "$PROXY" --limit-rate 120k -m 25 -T /dev/zero "https://speed.cloudflare.com/__up" &
	for u in https://www.google.com https://github.com https://www.netflix.com https://cp.cloudflare.com/generate_204; do
		curl -s -o /dev/null -x "$PROXY" -m 5 "$u" &
	done
	wait
done
