#!/bin/sh
set -eu
cd "$(dirname "$0")"

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
bundle="$stage/DOTA 内战局.app"
mkdir -p "$bundle/Contents/MacOS"

export GOOS=darwin CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=13.0
export CGO_CFLAGS="${CGO_CFLAGS:--O2 -g} -mmacosx-version-min=13.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:--O2 -g} -mmacosx-version-min=13.0"
for arch in amd64 arm64; do
    GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "$stage/dota-shuffle-$arch" .
done
lipo -create "$stage/dota-shuffle-amd64" "$stage/dota-shuffle-arm64" -output "$bundle/Contents/MacOS/dota-shuffle"

cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key><string>dota-shuffle</string>
    <key>CFBundleIdentifier</key><string>com.zc.dota-shuffle</string>
    <key>CFBundleName</key><string>DOTA 内战局</string>
    <key>CFBundleDisplayName</key><string>DOTA 内战局</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>CFBundleShortVersionString</key><string>1.0</string>
    <key>CFBundleVersion</key><string>1</string>
    <key>LSMinimumSystemVersion</key><string>13.0</string>
    <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

codesign --force --sign "${DOTA_MAC_SIGN_IDENTITY:--}" "$bundle"
ditto "$bundle" "DOTA 内战局.app"
codesign --verify --strict "DOTA 内战局.app"
