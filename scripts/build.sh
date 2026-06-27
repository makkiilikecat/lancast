#!/usr/bin/env bash
# 各 OS 向けバイナリをネイティブビルドする。
# cgo を使うためクロスコンパイルはせず、mac はローカル・Linux は SSH 先でビルドする。
#
# 使い方: scripts/build.sh
# 環境変数:
#   LINUX_SSH   Linux ビルド先の SSH ホスト（既定: i7-7700HQ.ud）
#   LINUX_GO    Linux 側の go パス（既定: /usr/local/go/bin/go）
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
mkdir -p bin

LINUX_SSH="${LINUX_SSH:-i7-7700HQ.ud}"
LINUX_GO="${LINUX_GO:-/usr/local/go/bin/go}"

echo "== macOS (native) =="
GOOS_LOCAL="$(go env GOOS)"
GOARCH_LOCAL="$(go env GOARCH)"
go build -o "bin/lancast-${GOOS_LOCAL}-${GOARCH_LOCAL}" .
echo "  -> bin/lancast-${GOOS_LOCAL}-${GOARCH_LOCAL}"

if [ "$GOOS_LOCAL" = "darwin" ]; then
  echo "== macOS .app bundle =="
  APP="bin/LANCast.app"
  rm -rf "$APP"; mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
  cp "bin/lancast-${GOOS_LOCAL}-${GOARCH_LOCAL}" "$APP/Contents/MacOS/lancast"

  # assets/icon.png -> icon.icns（iconutil は標準搭載）。
  if [ -f assets/icon.png ]; then
    ICONSET="$(mktemp -d)/icon.iconset"; mkdir -p "$ICONSET"
    for s in 16 32 64 128 256 512; do
      sips -z $s $s assets/icon.png --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
      sips -z $((s*2)) $((s*2)) assets/icon.png --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
    done
    iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/icon.icns"
  fi

  cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>LANCast</string>
  <key>CFBundleDisplayName</key><string>LANCast</string>
  <key>CFBundleExecutable</key><string>lancast</string>
  <key>CFBundleIdentifier</key><string>dev.makkii.lancast</string>
  <key>CFBundleIconFile</key><string>icon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST
  echo "  -> $APP"
fi

echo "== Linux (native via SSH: $LINUX_SSH) =="
if ssh -o ConnectTimeout=6 "$LINUX_SSH" true 2>/dev/null; then
  rsync -az --delete --exclude bin --exclude .git "$ROOT/" "$LINUX_SSH:~/lancast-src/"
  ssh "$LINUX_SSH" "cd ~/lancast-src && $LINUX_GO build -o lancast-linux-amd64 ."
  scp -q "$LINUX_SSH:~/lancast-src/lancast-linux-amd64" bin/lancast-linux-amd64
  echo "  -> bin/lancast-linux-amd64"
else
  echo "  SKIP: $LINUX_SSH に接続できません"
fi

echo "完了:"
ls -lh bin/
