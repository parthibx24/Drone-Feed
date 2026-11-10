#!/usr/bin/env bash
# Run the tests and build everything into dist/:
#
#   dist/linux-arm64/{gimbal-bridge,drone-gateway,sim-vehicle}
#   dist/linux-amd64/{drone-gateway,sim-vehicle}
#   dist/pi-bundle/   everything drone/deploy/install.sh needs on a drone Pi
#
# The central server is built with Docker instead (see Dockerfile).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
VERSION="${VERSION:-$(date +%Y.%m.%d)-$(git rev-parse --short HEAD 2>/dev/null || echo local)}"
LDFLAGS="-s -w -X main.version=$VERSION"

MAVLINK_ROUTER_URL="https://github.com/mavlink-router/mavlink-router/releases/download/v4/mavlink-routerd-glibc-aarch64"
MAVLINK_ROUTER_CACHE="$ROOT/dist/cache/mavlink-routerd-v4-glibc-aarch64"

build() { # os arch package-dir output-name
  mkdir -p "dist/$1-$2"
  CGO_ENABLED=0 GOOS="$1" GOARCH="$2" go build -trimpath -ldflags "$LDFLAGS" -o "dist/$1-$2/$4" "./$3"
}

echo "== go test"
go test ./...

echo "== building $VERSION"
build linux arm64 drone/cmd/gimbal-bridge gimbal-bridge
build linux arm64 server/cmd/drone-gateway drone-gateway
build linux amd64 server/cmd/drone-gateway drone-gateway
build linux arm64 tools/sim-vehicle sim-vehicle
build linux amd64 tools/sim-vehicle sim-vehicle

if [[ ! -s "$MAVLINK_ROUTER_CACHE" ]]; then
  echo "== downloading mavlink-router v4 (aarch64)"
  mkdir -p "$(dirname "$MAVLINK_ROUTER_CACHE")"
  curl -fL --retry 3 -o "$MAVLINK_ROUTER_CACHE" "$MAVLINK_ROUTER_URL"
fi

echo "== assembling dist/pi-bundle"
B="dist/pi-bundle"
rm -rf "$B"
mkdir -p "$B/bin"
cp "dist/linux-arm64/gimbal-bridge" "$B/bin/"
cp "$MAVLINK_ROUTER_CACHE" "$B/bin/mavlink-routerd"
chmod +x "$B/bin/"*
cp -R drone/deploy/mavlink-router drone/deploy/systemd "$B/"
cp drone/deploy/gimbal-bridge.env drone/deploy/install.sh drone/deploy/rollback.sh "$B/"
chmod +x "$B/install.sh" "$B/rollback.sh"

ls -lh dist/linux-arm64 dist/linux-amd64 "$B/bin"
