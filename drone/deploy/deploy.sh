#!/usr/bin/env bash
# Build the Pi bundle on your Mac, copy it to a drone, and run install.sh there.
#
#   ./drone/deploy/deploy.sh dronepi@100.111.237.109 transition
#
# You will be asked for the Pi's sudo password.
set -euo pipefail

TARGET="${1:?usage: $0 user@host transition|final}"
MODE="${2:?usage: $0 user@host transition|final}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

"$ROOT/scripts/build.sh"
BUNDLE="$ROOT/dist/pi-bundle"

echo "== copying bundle to $TARGET"
ssh "$TARGET" 'rm -rf /tmp/drone-stack-bundle && mkdir -p /tmp/drone-stack-bundle'
scp -q -r "$BUNDLE"/. "$TARGET:/tmp/drone-stack-bundle/"

echo "== installing ($MODE)"
ssh -t "$TARGET" "cd /tmp/drone-stack-bundle && sudo ./install.sh $MODE"
