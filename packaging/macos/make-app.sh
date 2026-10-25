#!/bin/bash
# Đóng gói binary thành Tui Tai.app (chạy trên macOS runner).
# Dùng: ./make-app.sh <binary> <version> <output-dir>
set -euo pipefail
BIN="${1:?binary}"; VER="${2:-v0.0.1}"; OUT="${3:-dist}"
APP="$OUT/Tui Tai.app"
rm -rf "$APP"; mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN" "$APP/Contents/MacOS/tuitai"; chmod +x "$APP/Contents/MacOS/tuitai"
if [ -f "packaging/macos/AppIcon.png" ]; then
  mkdir -p icon.iconset
  for s in 16 32 64 128 256 512; do
    sips -z $s $s packaging/macos/AppIcon.png --out "icon.iconset/icon_${s}x${s}.png" >/dev/null
    d=$((s*2)); sips -z $d $d packaging/macos/AppIcon.png --out "icon.iconset/icon_${s}x${s}@2x.png" >/dev/null
  done
  iconutil -c icns icon.iconset -o "$APP/Contents/Resources/AppIcon.icns" || true
fi
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>Tui Tai</string>
  <key>CFBundleDisplayName</key><string>Tui Tải</string>
  <key>CFBundleIdentifier</key><string>com.captajn.tuitai</string>
  <key>CFBundleVersion</key><string>${VER}</string>
  <key>CFBundleShortVersionString</key><string>${VER}</string>
  <key>CFBundleExecutable</key><string>tuitai</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
echo "built: $APP"
