#!/usr/bin/env bash
set -euo pipefail

# Wait for a device to be reachable, then issue a single Shelly Gen2 reboot RPC.
# Attempts run immediately and then every INTERVAL_SEC seconds (default: 300).
# Logs each attempt with date & time. Exits after a successful reboot request (HTTP 2xx).
#
# Usage:
#   tools/wait-and-reboot.sh [IP]
#   IP: Device IP (default: 192.168.1.75)
#
# Env:
#   INTERVAL_SEC: Interval between attempts in seconds (default: 300)
#   CURL_TIMEOUT: curl timeout in seconds (default: 10)
#
# Example:
#   INTERVAL_SEC=600 tools/wait-and-reboot.sh 192.168.1.75

IP="${1:-192.168.1.75}"
INTERVAL_SEC="${INTERVAL_SEC:-300}"
CURL_TIMEOUT="${CURL_TIMEOUT:-10}"

log() {
  # shellcheck disable=SC2059
  printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  sed -n '1,40p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
fi

while :; do
  log "Attempting reboot on ${IP}"
  if ping -c1 -W1 "$IP" >/dev/null 2>&1; then
    code=$(curl -sS -m "$CURL_TIMEOUT" -o /dev/null -w '%{http_code}' \
      -X POST "http://$IP/rpc/Shelly.Reboot" \
      -H "Content-Type: application/json" \
      -d '{"delay_ms":0,"reason":"user_request"}') || code=000
    log "Reboot request HTTP ${code}"
    if [[ "$code" -ge 200 && "$code" -lt 300 ]]; then
      log "Reboot triggered successfully. Exiting."
      exit 0
    fi
  fi
  sleep "$INTERVAL_SEC"
done
