#!/usr/bin/env bash
# LANCast を各マシンのアプリ一覧へインストール／アップデートする。
# 再実行すれば最新ソースから再ビルドして上書き＝アップデートになる。
#
#   ローカル(mac) : /Applications/LANCast.app（Launchpad / Spotlight に表示）
#   リモート(linux): ~/.local/bin/lancast + .desktop（アプリ一覧に表示）
#
# 使い方: scripts/install.sh
# 環境変数:
#   LINUX_SSH   Linux インストール先の SSH ホスト（既定: i7-7700HQ.ud。空なら remote をスキップ）
#   LINUX_GO    Linux 側の go パス（既定: /usr/local/go/bin/go）
set -euo pipefail

cd "$(dirname "$0")/.."
LINUX_SSH="${LINUX_SSH:-i7-7700HQ.ud}"
LINUX_GO="${LINUX_GO:-/usr/local/go/bin/go}"

# Linux 上で直接実行した場合のローカルインストール。
install_linux_local() {
  echo "== install (Linux local) =="
  mkdir -p ~/.local/bin ~/.local/share/lancast ~/.local/share/applications
  # 稼働中でも上書きできるよう一時ファイル + atomic rename。
  install -m755 "bin/lancast-linux-$(go env GOARCH)" ~/.local/bin/lancast.new
  mv -f ~/.local/bin/lancast.new ~/.local/bin/lancast
  cp assets/icon.png ~/.local/share/lancast/icon.png
  cat > ~/.local/share/applications/lancast.desktop <<DESKTOP
[Desktop Entry]
Type=Application
Name=LANCast
Comment=LAN screen cast to virtual camera (Discord)
Exec=$HOME/.local/bin/lancast
Icon=$HOME/.local/share/lancast/icon.png
Terminal=false
Categories=AudioVideo;Network;
DESKTOP
  update-desktop-database ~/.local/share/applications 2>/dev/null || true
  echo "  -> アプリ一覧から「LANCast」で起動できます"
}

# 1) 全 OS 向けにビルド（mac ローカル / Linux は SSH 先でネイティブビルド）。
LINUX_SSH="$LINUX_SSH" LINUX_GO="$LINUX_GO" ./scripts/build.sh

# 2) ローカル OS へインストール。
case "$(go env GOOS)" in
darwin)
  DEST="/Applications/LANCast.app"
  echo "== install (macOS): $DEST =="
  rm -rf "$DEST"
  cp -R bin/LANCast.app "$DEST"
  # LaunchServices へ登録（Spotlight/Launchpad に即反映）。
  /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$DEST" 2>/dev/null || true
  echo "  -> Launchpad / Spotlight から「LANCast」で起動できます"
  ;;
linux)
  install_linux_local
  ;;
esac

# 3) リモート Linux へインストール（到達できる場合）。
if [ -n "$LINUX_SSH" ] && ssh -o ConnectTimeout=6 "$LINUX_SSH" true 2>/dev/null; then
  echo "== install (Linux via SSH: $LINUX_SSH) =="
  scp -q assets/icon.png "$LINUX_SSH:/tmp/lancast-icon.png"
  ssh "$LINUX_SSH" 'bash -s' <<'REMOTE'
set -e
mkdir -p ~/.local/bin ~/.local/share/lancast ~/.local/share/applications
# 稼働中でも上書きできるよう一時ファイル + atomic rename。
install -m755 ~/lancast-src/lancast-linux-amd64 ~/.local/bin/lancast.new
mv -f ~/.local/bin/lancast.new ~/.local/bin/lancast
mv -f /tmp/lancast-icon.png ~/.local/share/lancast/icon.png
cat > ~/.local/share/applications/lancast.desktop <<DESKTOP
[Desktop Entry]
Type=Application
Name=LANCast
Comment=LAN screen cast to virtual camera (Discord)
Exec=$HOME/.local/bin/lancast
Icon=$HOME/.local/share/lancast/icon.png
Terminal=false
Categories=AudioVideo;Network;
DESKTOP
update-desktop-database ~/.local/share/applications 2>/dev/null || true
echo "  -> アプリ一覧(GNOME)から「LANCast」で起動できます: ~/.local/bin/lancast"
REMOTE
else
  echo "== install (Linux): SKIP（$LINUX_SSH に接続できません）=="
fi

echo "完了。アップデートはこのスクリプトを再実行してください。"
