#!/usr/bin/env bash
# Return a Pi to the old stack (MAVProxy + Python drone-agent). Run ON THE PI
# with sudo. Leaves the new files in place so install.sh can be re-run.
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run with sudo" >&2; exit 1; }

systemctl disable --now gimbal-bridge.service 2>/dev/null || true
systemctl disable --now mavlink-router.service 2>/dev/null || true
systemctl enable --now mavproxy.service
systemctl enable --now drone-agent.service

sleep 3
for s in mavproxy drone-agent mavlink-router gimbal-bridge go2rtc; do
  printf '%-16s %s\n' "$s" "$(systemctl is-active "$s" 2>/dev/null || true)"
done
