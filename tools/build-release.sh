#!/bin/sh
set -eu
cd "$(dirname "$0")/rstack"
mkdir -p ../../dist
for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
  GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 GOTOOLCHAIN=local \
    go build -trimpath -ldflags='-s -w' -o "../../dist/rstack-${target%/*}-${target#*/}" .
done
cd ../../dist
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum rstack-* > SHA256SUMS
else
  shasum -a 256 rstack-* > SHA256SUMS
fi
