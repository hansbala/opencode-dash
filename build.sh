#!/usr/bin/env sh
set -eu

DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
OUT="$DIR/build"

if ! command -v go >/dev/null 2>&1; then
  echo "error: go not found (need Go 1.22+)" >&2
  exit 1
fi

mkdir -p "$OUT"

if [ "$(uname -s)" = "Darwin" ]; then
  if ! command -v swiftc >/dev/null 2>&1; then
    echo "error: swiftc not found (install Xcode Command Line Tools)" >&2
    exit 1
  fi

  APP="$OUT/OpenCodeDash.app"
  rm -rf "$APP"
  mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
  cp "$DIR/swift/Info.plist" "$APP/Contents/Info.plist"

  swiftc -O -parse-as-library \
    -o "$APP/Contents/MacOS/OpenCodeDash" \
    "$DIR/swift/OpenCodeAPI.swift" \
    "$DIR/swift/OpenCodeDashApp.swift" \
    -framework SwiftUI -framework AppKit -framework Foundation

  codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || true
  echo "Built $APP"
fi

go build -C "$DIR/go" -trimpath -ldflags="-s -w" -o "$OUT/opencode-dash" .
echo "Built $OUT/opencode-dash"
