#!/usr/bin/env sh
# Build script for Linux, macOS and Git Bash on Windows.
#
#   ./build.sh            build for the current OS/architecture -> build/resume-generator[.exe]
#   ./build.sh all        cross-compile linux, windows and macOS binaries into build/
#   ./build.sh test       run gofmt check, go vet and go test
set -eu

ROOT="$(cd "$(dirname "$0")" && pwd)"
CODE="$ROOT/code"
OUT="$ROOT/build"
NAME="resume-generator"
VERSION="$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)"
LDFLAGS="-s -w -X main.version=$VERSION"

mkdir -p "$OUT"
export CGO_ENABLED=0

build_one() { # GOOS GOARCH OUTPUT
  echo "building $3"
  (cd "$CODE" && GOOS="$1" GOARCH="$2" go build -trimpath -ldflags "$LDFLAGS" -o "$3" ./cmd/server)
}

case "${1:-}" in
  "")
    ext=""
    [ "$(cd "$CODE" && go env GOOS)" = "windows" ] && ext=".exe"
    build_one "$(cd "$CODE" && go env GOOS)" "$(cd "$CODE" && go env GOARCH)" "$OUT/$NAME$ext"
    ;;
  all)
    build_one linux   amd64 "$OUT/$NAME-linux-amd64"
    build_one linux   arm64 "$OUT/$NAME-linux-arm64"
    build_one windows amd64 "$OUT/$NAME-windows-amd64.exe"
    build_one darwin  amd64 "$OUT/$NAME-darwin-amd64"
    build_one darwin  arm64 "$OUT/$NAME-darwin-arm64"
    ;;
  test)
    cd "$CODE"
    unformatted="$(gofmt -l .)"
    if [ -n "$unformatted" ]; then echo "gofmt needed:"; echo "$unformatted"; exit 1; fi
    go vet ./...
    go test ./...
    ;;
  *)
    echo "usage: $0 [all|test]" >&2
    exit 2
    ;;
esac
