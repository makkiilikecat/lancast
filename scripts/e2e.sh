#!/usr/bin/env bash
# LANCast の実機 E2E テスト。
# Mac(Host) → UDP → Linux(Client) → v4l2loopback /dev/video10 を実際に通し、
# 仮想カメラから1フレーム取得して「実映像が流れているか」まで検証する。
#
# 前提: 本スクリプトを Mac(送信側) で実行。Linux(受信側) へ SSH 到達できること。
#       Linux に v4l2loopback (git 0.15+) が導入済みであること。
#
# 使い方:
#   scripts/e2e.sh                       # 既定 1280x720/30fps/20Mbps/hevc_videotoolbox
#   scripts/e2e.sh --size 1920x1080 --fps 30 --encoder hevc_videotoolbox --bitrate 15000
#
# 環境変数: LINUX_SSH(既定 i7-7700HQ.ud), PORT(既定 5004), DEVICE(既定 /dev/video10)
set -uo pipefail

cd "$(dirname "$0")/.."
LINUX_SSH="${LINUX_SSH:-i7-7700HQ.ud}"
PORT="${PORT:-5004}"
DEVICE="${DEVICE:-/dev/video10}"
SIZE="1280x720"; FPS="30"; ENCODER="hevc_videotoolbox"; BITRATE="20000"
WAIT_SECS="6"

while [ $# -gt 0 ]; do
  case "$1" in
    --size) SIZE="$2"; shift 2;;
    --fps) FPS="$2"; shift 2;;
    --encoder) ENCODER="$2"; shift 2;;
    --bitrate) BITRATE="$2"; shift 2;;
    *) echo "不明な引数: $1"; exit 2;;
  esac
done

MAC_BIN="bin/lancast-darwin-arm64"
HOST_LOG="$(mktemp -d)/host.log"
PASS=0; FAIL=0
ok()   { echo "  ✅ $1"; PASS=$((PASS+1)); }
ng()   { echo "  ❌ $1"; FAIL=$((FAIL+1)); }
info() { echo "— $1"; }

# remote_clean は Linux 側の lancast/ffmpeg/ポート占有を確実に除去する。
# client を強制終了すると子 ffmpeg が孤児化してポートを掴むため fuser -k で掃除する。
# プロセス名は -x lancast で厳密一致させる（-f だと ssh の bash コマンド自身に
# 誤マッチするため）。
remote_clean() {
  ssh "$LINUX_SSH" "pkill -INT -x lancast 2>/dev/null; sleep 1; \
    pkill -9 -x lancast 2>/dev/null; pkill -9 -f 'lancast-linux-amd64' 2>/dev/null; \
    fuser -k ${PORT}/udp 2>/dev/null; true" 2>/dev/null
}

cleanup() {
  info "クリーンアップ"
  [ -n "${HOST_PID:-}" ] && kill -INT "$HOST_PID" 2>/dev/null
  remote_clean
  pkill -f "lancast-darwin-arm64 -host" 2>/dev/null
  sleep 1
}
trap cleanup EXIT

echo "=== LANCast E2E: $SIZE / ${FPS}fps / ${BITRATE}k / $ENCODER → $LINUX_SSH:$DEVICE ==="

[ -x "$MAC_BIN" ] || { echo "ビルドしてください: go build -o $MAC_BIN ."; exit 2; }

# 0) 事前クリーンアップ（残プロセス・ポート占有を除去）
info "事前クリーンアップ"
pkill -f "lancast-darwin-arm64 -host" 2>/dev/null
remote_clean
ssh "$LINUX_SSH" 'sudo modprobe v4l2loopback 2>/dev/null; true'
# ポートが解放されるまで待つ（孤児プロセスの残存対策）
for i in $(seq 1 10); do
  ssh "$LINUX_SSH" "ss -ulpn 2>/dev/null | grep -q :$PORT" || break
  sleep 0.5
done
if ssh "$LINUX_SSH" "ss -ulpn 2>/dev/null | grep -q :$PORT"; then
  echo "  ⚠ :$PORT が解放されません。手動で確認してください。"; exit 1
fi

# 1) Client(受信) を Linux でヘッドレス起動
info "Client 起動 (Linux)"
ssh "$LINUX_SSH" "nohup ~/.local/bin/lancast -client -port $PORT -device $DEVICE -debug >/tmp/lc-e2e-client.log 2>&1 & echo started"
# 受信待機になるまで待つ
for i in $(seq 1 10); do
  if ssh "$LINUX_SSH" "ss -ulpn 2>/dev/null | grep -q :$PORT"; then break; fi
  sleep 0.5
done
if ssh "$LINUX_SSH" "ss -ulpn 2>/dev/null | grep -q :$PORT"; then ok "Client が :$PORT で待機"; else ng "Client がポートを開けない"; ssh "$LINUX_SSH" "tail -5 /tmp/lc-e2e-client.log"; exit 1; fi

# 2) Host(送信) を Mac でヘッドレス起動
info "Host 起動 (Mac)"
nohup "$MAC_BIN" -host -dest "$(ssh "$LINUX_SSH" 'hostname -I | awk "{print \$1}"')" -port "$PORT" \
  -size "$SIZE" -fps "$FPS" -encoder "$ENCODER" -bitrate "$BITRATE" -debug >"$HOST_LOG" 2>&1 &
HOST_PID=$!

info "${WAIT_SECS}秒 ストリーム確立を待機"
sleep "$WAIT_SECS"

# 3) 送受信が進んでいるか（frame= ログ）
grep -q "frame=" "$HOST_LOG" && ok "Host が送信中 (frame=)" || { ng "Host が送信していない"; tail -8 "$HOST_LOG"; }
ssh "$LINUX_SSH" "grep -q 'frame=' /tmp/lc-e2e-client.log" && ok "Client が受信中 (frame=)" || { ng "Client が受信していない"; ssh "$LINUX_SSH" "tail -8 /tmp/lc-e2e-client.log"; }

# 4) 仮想カメラから1フレーム取得し検証
info "仮想カメラ $DEVICE からフレーム取得・検証"
W="${SIZE%x*}"; H="${SIZE#*x}"
# 仮想カメラから1フレーム取得し、解像度とファイルサイズで検証する。
# 単色/ブランクの PNG は数 KB に圧縮されるため、サイズが十分大きければ実映像とみなす。
VALID=$(ssh "$LINUX_SSH" "ffmpeg -hide_banner -loglevel error -f v4l2 -i $DEVICE -frames:v 1 -y /tmp/lc-e2e-frame.png 2>/dev/null; \
if [ ! -s /tmp/lc-e2e-frame.png ]; then echo NOFILE; else \
  DIM=\$(ffprobe -v error -select_streams v -show_entries stream=width,height -of csv=p=0:s=x /tmp/lc-e2e-frame.png); \
  SZ=\$(stat -c%s /tmp/lc-e2e-frame.png); \
  echo \"DIM=\$DIM SIZE=\$SZ\"; \
fi")
echo "    $VALID"
case "$VALID" in
  *NOFILE*) ng "フレームを取得できなかった";;
  *"DIM=${W}x${H}"*) ok "解像度一致 (${W}x${H})";;
  *) ng "解像度不一致 (期待 ${W}x${H})";;
esac
SZVAL=$(echo "$VALID" | sed -n 's/.*SIZE=\([0-9]*\).*/\1/p')
if [ -n "$SZVAL" ] && [ "$SZVAL" -gt 20000 ]; then ok "実映像あり (PNG ${SZVAL}B > 20KB=非ブランク)"; else ng "ブランク/単色の疑い (PNG ${SZVAL:-?}B)"; fi
scp -q "$LINUX_SSH:/tmp/lc-e2e-frame.png" "/tmp/lc-e2e-frame.png" 2>/dev/null && info "取得フレーム: /tmp/lc-e2e-frame.png"

# 5) graceful 停止と後始末の検証
info "停止検証"
kill -INT "$HOST_PID" 2>/dev/null; sleep 3
if pgrep -f "lancast-darwin-arm64 -host" >/dev/null; then ng "Host が停止しない"; else ok "Host graceful 停止"; fi
RES=$(ssh "$LINUX_SSH" "pkill -INT -x lancast; sleep 5; pgrep -x lancast >/dev/null && echo RUNNING || echo STOPPED; ss -ulpn 2>/dev/null | grep -q :$PORT && echo BOUND || echo FREE")
st=$(echo "$RES" | sed -n 1p); pb=$(echo "$RES" | sed -n 2p)
[ "$st" = STOPPED ] && ok "Client graceful 停止" || ng "Client が停止しない (=$st)"
[ "$pb" = FREE ] && ok "ポート :$PORT 解放" || ng "ポートが解放されない (=$pb)"

echo "=== 結果: PASS=$PASS FAIL=$FAIL ==="
[ "$FAIL" -eq 0 ]
