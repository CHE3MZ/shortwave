#!/usr/bin/env bash
#
# Cross-platform release builds for shortwave.
#
#   ./scripts/build.sh [version]
#
# Builds into dist/:
#   dist/shortwave-<version>-<os>-<arch>[.exe] + dist/SHA256SUMS
#
# windows/* and darwin/* always build (CGO-free). linux/* needs CGO + ALSA
# (libasound2-dev) because the audio driver has no pure-Go Linux backend, so
# Linux targets only build on a Linux host; elsewhere they are skipped with a
# note. Set SHORTWAVE_BUILD_LINUX=1 to force the attempt anyway.
#
# The optional version only names artifacts; the binary reports the version
# from internal/version. Run from anywhere; always operates on the repo root.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${1:-$(grep -o '"[0-9][0-9.]*"' internal/version/version.go | head -n1 | tr -d '"')}"
VERSION="${VERSION:-0.0.0-dev}"
PKG="./cmd/shortwave"
HOST_OS="$(go env GOOS)"

mkdir -p dist
rm -f dist/SHA256SUMS

# os/arch/cgo triples to build.
MATRIX="
windows/amd64/0
windows/arm64/0
darwin/amd64/0
darwin/arm64/0
linux/amd64/1
linux/arm64/1
"

echo "building shortwave ${VERSION} into dist/ (host: ${HOST_OS})"
# shellcheck disable=SC2086
for triple in ${MATRIX}; do
    os="${triple%/*/*}"; rest="${triple#*/}"; arch="${rest%/*}"; cgo="${rest#*/}"
    out="dist/shortwave-${VERSION}-${os}-${arch}"
    if [ "${os}" = "windows" ]; then
        out="${out}.exe"
    fi
    if [ "${os}" = "linux" ] && [ "${HOST_OS}" != "linux" ] && [ "${SHORTWAVE_BUILD_LINUX:-0}" != "1" ]; then
        echo "  -- skip ${out} (needs a Linux host with CGO + ALSA; override with SHORTWAVE_BUILD_LINUX=1)"
        continue
    fi
    echo "  -> ${out}"
    CGO_ENABLED="${cgo}" GOOS="${os}" GOARCH="${arch}" \
        go build -trimpath -ldflags "-s -w" -o "${out}" "${PKG}"
done

# Checksums for release uploads (SHA256SUMS itself never matches shortwave-*).
if command -v sha256sum >/dev/null 2>&1; then
    (cd dist && sha256sum shortwave-* > SHA256SUMS)
elif command -v shasum >/dev/null 2>&1; then
    (cd dist && shasum -a 256 shortwave-* > SHA256SUMS)
else
    echo "warn: no sha256sum/shasum found, skipping checksums" >&2
    exit 0
fi

echo "wrote $(wc -l < dist/SHA256SUMS | tr -d ' ') checksums to dist/SHA256SUMS"
