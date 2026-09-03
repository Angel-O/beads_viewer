#!/usr/bin/env bash
# dashboard_browser_smoke.sh: load an exported dashboard in a headless
# Chromium and fail on any Content-Security-Policy refusal or uncaught error,
# while requiring the app's own boot markers (database cached, WASM graph
# engine loaded, charts initialised, triage loaded) to appear in the console.
#
# Usage: BV_HEADLESS_BROWSER=/path/to/chrome scripts/dashboard_browser_smoke.sh [repo-dir]
#        BV_BIN=/path/to/bv skips the build.  BV_BROWSER_SECONDS=30 sets the window.
# The bundle is served over loopback with COOP/COEP so the page is
# cross-origin isolated on first load (the state a real deployment reaches
# after the service worker installs); the COI bootstrap therefore does not
# reload, which is what makes a fixed observation window sufficient.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
target="${1:-$root}"
browser="${BV_HEADLESS_BROWSER:-}"
seconds="${BV_BROWSER_SECONDS:-30}"
[ -n "$browser" ] && [ -x "$browser" ] || { echo "dashboard_browser_smoke: set BV_HEADLESS_BROWSER to a Chromium/Chrome binary"; exit 2; }
command -v python3 >/dev/null || { echo "dashboard_browser_smoke: python3 is required"; exit 2; }
export BV_NO_BROWSER=1 BV_TEST_MODE=1 BV_NO_UPDATE_CHECK=1 BV_NO_SAVED_CONFIG=1

tmp="$(mktemp -d "${TMPDIR:-/tmp}/bv-browser-smoke.XXXXXX")"
server_pid=""
cleanup() { [ -n "$server_pid" ] && kill "$server_pid" 2>/dev/null; rm -rf "$tmp"; }
trap cleanup EXIT

bv="${BV_BIN:-}"
if [ -z "$bv" ]; then
  bv="$tmp/bv"
  (cd "$root" && go build -o "$bv" ./cmd/bv)
fi
(cd "$target" && "$bv" --export-pages "$tmp/bundle" --pages-title "browser smoke" >"$tmp/export.log" 2>&1) || { echo "export failed:"; tail -5 "$tmp/export.log"; exit 1; }

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
python3 - "$tmp/bundle" "$port" >"$tmp/server.log" 2>&1 <<'PY' &
import http.server, os, sys
class H(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Cross-Origin-Opener-Policy", "same-origin")
        self.send_header("Cross-Origin-Embedder-Policy", "require-corp")
        self.send_header("Cache-Control", "no-store")
        super().end_headers()
    def log_message(self, *a): pass
os.chdir(sys.argv[1])
http.server.ThreadingHTTPServer(("127.0.0.1", int(sys.argv[2])), H).serve_forever()
PY
server_pid=$!
for _ in $(seq 1 50); do curl -s -o /dev/null "http://127.0.0.1:$port/" && break; sleep 0.1; done

# Headless Chromium keeps running until killed; the console goes to stderr.
set +e
timeout -s INT "$seconds" "$browser" --headless=new --no-sandbox --disable-gpu --disable-dev-shm-usage \
  --enable-logging=stderr --v=0 --log-level=0 \
  --no-first-run --disable-background-networking --disable-sync --disable-component-update \
  --user-data-dir="$tmp/profile" "http://127.0.0.1:$port/" >/dev/null 2>"$tmp/console.log"
set -e

fail=0
echo "== CSP refusals"
if rg -n 'Refused to|Content Security Policy|violates the following' "$tmp/console.log" | cut -c1-200; then fail=1; else echo "(none)"; fi
echo "== uncaught errors"
if rg -n 'Uncaught|ReferenceError|TypeError|ERROR:CONSOLE' "$tmp/console.log" | cut -c1-200; then fail=1; else echo "(none)"; fi
echo "== boot markers"
for marker in '\[OPFS\] Cached' '\[Graph\] WASM engine loaded' '\[bv-charts\] Dashboard initialized' '\[Viewer\] Triage data loaded'; do
  if rg -q "$marker" "$tmp/console.log"; then
    printf '  ok      %s\n' "$(rg -o "$marker[^\"]*" "$tmp/console.log" | head -1)"
  else
    printf '  MISSING %s\n' "$marker"; fail=1
  fi
done
# Time from the first console line of the page to the triage-loaded marker,
# from Chromium's own log timestamps (MMDD/HHMMSS.micro). Informational: it
# is the closest thing to first-render this harness can measure without a
# DevTools client, and it is what tests/artifacts/perf/pages_load.json means
# by first_render_ms when a browser is available.
awk '
  function secs(ts,   h, m, s) { h = substr(ts, 6, 2); m = substr(ts, 8, 2); s = substr(ts, 10); return h * 3600 + m * 60 + s }
  match($0, /[0-9]{4}\/[0-9]{6}\.[0-9]+/) {
    ts = substr($0, RSTART, RLENGTH)
    if (first == "" && $0 ~ /CONSOLE/) first = ts
    if ($0 ~ /\[Viewer\] Triage data loaded/ && first != "") { printf "== time to triage loaded: %.0f ms (page console start to [Viewer] Triage data loaded)\n", (secs(ts) - secs(first)) * 1000; exit }
  }
' "$tmp/console.log"

if [ "$fail" -eq 0 ]; then
  echo "dashboard_browser_smoke: PASS (no CSP refusals, no uncaught errors, app booted)"
else
  echo "dashboard_browser_smoke: FAIL (console: $tmp/console.log kept? no - rerun with BV_BROWSER_KEEP=1)"
  if [ "${BV_BROWSER_KEEP:-0}" = "1" ]; then trap - EXIT; echo "kept $tmp"; fi
fi
exit "$fail"
