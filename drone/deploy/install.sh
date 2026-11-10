#!/usr/bin/env bash
# Install the new drone-side stack on a Raspberry Pi. Run ON THE PI with sudo,
# from the unpacked bundle directory (deploy.sh does this for you).
#
#   sudo ./install.sh transition   MAVProxy -> mavlink-router, add gimbal-bridge.
#                                  Old drone-agent keeps running (Drone-Feed still works).
#   sudo ./install.sh final        Also stop the old drone-agent; bridge takes UDP 5000.
#
# Undo with: sudo /opt/drone-stack/rollback.sh
set -euo pipefail

MODE="${1:-}"
if [[ "$MODE" != "transition" && "$MODE" != "final" ]]; then
  echo "usage: sudo $0 transition|final" >&2
  exit 2
fi
[[ $EUID -eq 0 ]] || { echo "run with sudo" >&2; exit 1; }
[[ "$(uname -m)" == "aarch64" ]] || { echo "expected aarch64, got $(uname -m)" >&2; exit 1; }

HERE="$(cd "$(dirname "$0")" && pwd)"
for f in bin/mavlink-routerd bin/gimbal-bridge mavlink-router/main.conf gimbal-bridge.env \
         systemd/mavlink-router.service systemd/gimbal-bridge.service; do
  [[ -f "$HERE/$f" ]] || { echo "missing $f in bundle" >&2; exit 1; }
done

echo "== binaries -> /opt/drone-stack/bin"
install -d -m 0755 /opt/drone-stack/bin
install -m 0755 "$HERE/bin/mavlink-routerd" "$HERE/bin/gimbal-bridge" /opt/drone-stack/bin/
# Keep rollback.sh somewhere that survives a reboot (/tmp is RAM on the Pi).
install -m 0755 "$HERE/rollback.sh" /opt/drone-stack/rollback.sh

echo "== configuration"
install -d -m 0755 /etc/mavlink-router /etc/drone-stack
if [[ -f /etc/mavlink-router/main.conf ]] && ! cmp -s "$HERE/mavlink-router/main.conf" /etc/mavlink-router/main.conf; then
  cp /etc/mavlink-router/main.conf "/etc/mavlink-router/main.conf.bak.$(date +%s)"
fi
install -m 0644 "$HERE/mavlink-router/main.conf" /etc/mavlink-router/main.conf

TS_IP="$(tailscale ip -4 2>/dev/null | head -1 || true)"
[[ -n "$TS_IP" ]] || TS_IP="127.0.0.1"
LOCAL_PORT=5001
[[ "$MODE" == "final" ]] && LOCAL_PORT=5000
if [[ ! -f /etc/drone-stack/gimbal-bridge.env ]]; then
  sed -e "s/__TAILSCALE_IP__/$TS_IP/g" "$HERE/gimbal-bridge.env" > /etc/drone-stack/gimbal-bridge.env
fi
sed -i "s/^LOCAL_PORT=.*/LOCAL_PORT=$LOCAL_PORT/" /etc/drone-stack/gimbal-bridge.env
chmod 0644 /etc/drone-stack/gimbal-bridge.env

install -d -o dronepi -g dronepi /home/dronepi/flightlogs

echo "== systemd units"
install -m 0644 "$HERE/systemd/mavlink-router.service" "$HERE/systemd/gimbal-bridge.service" /etc/systemd/system/
systemctl daemon-reload

echo "== switching MAVProxy -> mavlink-router"
systemctl disable --now mavproxy.service 2>/dev/null || true
systemctl enable --now mavlink-router.service
# Never leave the flight controller's serial port without an owner: if the
# router does not stay up, put MAVProxy back and stop here.
sleep 5
if ! systemctl is-active --quiet mavlink-router.service \
   || [[ "$(systemctl show -p NRestarts --value mavlink-router.service)" != "0" ]]; then
  echo "!! mavlink-router did not stay up; restoring MAVProxy" >&2
  journalctl -u mavlink-router -n 20 --no-pager >&2 || true
  systemctl disable --now mavlink-router.service 2>/dev/null || true
  systemctl enable --now mavproxy.service
  exit 1
fi

if [[ "$MODE" == "final" ]]; then
  echo "== retiring the old drone-agent"
  systemctl disable --now drone-agent.service 2>/dev/null || true
fi
systemctl enable gimbal-bridge.service
systemctl restart gimbal-bridge.service

sleep 3
echo
for s in mavlink-router gimbal-bridge drone-agent mavproxy go2rtc; do
  printf '%-16s %s\n' "$s" "$(systemctl is-active "$s" 2>/dev/null || true)"
done
echo
echo "Mode: $MODE. gimbal-bridge local UDP port: $LOCAL_PORT. Advertised video host: $TS_IP"
echo "Logs:     journalctl -u mavlink-router -u gimbal-bridge -f"
echo "Rollback: sudo /opt/drone-stack/rollback.sh"
