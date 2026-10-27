#!/usr/bin/env bash
#
# Builds tglab from this checkout, with `git describe` as the version that --version prints.
#
# Usage: scripts/build.sh [output]   (default: ./tglab)
set -euo pipefail

cd "$(dirname "$0")/.."

name=tglab
out="${1:-./$name}"
version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

go build -trimpath -ldflags "-X main.version=$version" -o "$out" .
echo "built $out ($name $version)"
