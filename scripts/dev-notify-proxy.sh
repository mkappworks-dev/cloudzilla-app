#!/bin/sh
# Refresh the browser once the rebuilt server is answering, not when the file
# changed. templ's proxy would otherwise reload while air is still building,
# showing the previous binary's output; this watches /healthz instead, which
# probeMux answers ahead of the auth and setup middleware.
set -u

PORT="${CZ_SERVER_PORT:-8080}"
TEMPL="${TEMPL:-$HOME/go/bin/templ}"

# A curl spawn costs ~5.5ms, so a 1s tick is 0.6% of a core for the session
# against 2.8% at 200ms. The latency it adds is invisible behind a rebuild that
# takes seconds.
INTERVAL=1

# Assume up, so starting this beside an already-running server is not a reload.
up=1

while :; do
  if curl -fsS -o /dev/null --max-time 2 "http://127.0.0.1:${PORT}/healthz" 2>/dev/null; then
    if [ "$up" = 0 ]; then
      "$TEMPL" generate --notify-proxy ./internal/view/... >/dev/null 2>&1
    fi
    up=1
  else
    up=0
  fi
  sleep "$INTERVAL"
done
