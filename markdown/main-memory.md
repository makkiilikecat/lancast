# main-memory — LANCast 開発の経緯と決定事項

このファイルは LANCast が生まれた経緯・背景・主要な技術判断をまとめる作業メモ。
新しくこのリポジトリに関わるときは最初にここを読む。

## 発端: Mac→Linux 画面共有 → Discord 配信

元の目的は「**MacBook Neo の内蔵ディスプレイを LAN 経由で Ubuntu に転送し、Discord で画面配信**」。
Mac のリソース消費を最小に保つことが最優先要件。

### 確立した構成（手動運用で動作確認済み）

- **Host = MacBook Neo**（Apple A18 Pro / macOS 26 Tahoe）
  - `ffmpeg -f avfoundation -capture_cursor 1 -i "3:none" -vf scale=1280:720,fps=30 -c:v hevc_videotoolbox -b:v 20000k -realtime 1 -tag:v hvc1 -f mpegts udp://192.168.0.215:5004`
  - HEVC ハードウェアエンコード（videotoolbox）で CPU 負荷最小。
  - **教訓**: avfoundation スクリーンキャプチャに `-framerate` を付けると
    "Configuration of video device failed" になる。FPS は `-vf fps=30` で正規化する。
- **Client = Ubuntu (`i7-7700HQ.ud` / 192.168.0.215 / kernel 6.17.0-35)**
  - `ffmpeg -fflags nobuffer -flags low_delay -probesize 500000 -analyzeduration 0 -i "udp://@:5004?fifo_size=1000000&overrun_nonfatal=1" -pix_fmt yuv420p -f v4l2 /dev/video10`
  - 受信 → v4l2loopback 仮想カメラ `/dev/video10` → Discord のカメラ「MacScreen」。

### v4l2loopback の決定的な問題と解決

- 症状: ffmpeg から v4l2loopback への書き込みが `VIDIOC_G_FMT: Invalid argument` で失敗。
- 原因: **apt 版 v4l2loopback 0.12.7 が kernel 6.17 + ffmpeg 6.1 と非互換**。
- 解決: **git 版 0.15.4 を DKMS で導入**して解消。あわせて以下を恒久化（Ubuntu 側）:
  - `/etc/modprobe.d/v4l2loopback.conf`: `options v4l2loopback devices=1 video_nr=10 card_label="MacScreen" exclusive_caps=1`
  - `/etc/modules-load.d/v4l2loopback.conf`: 起動時自動ロード
  - `/etc/udev/rules.d/90-v4l2loopback.rules`: `/dev/video10` を video グループ 0660
  - ユーザー `makkii` を `video` グループに追加
- **教訓**: `exclusive_caps=1` では writer が接続するまで Output デバイスとして振る舞い、
  接続後に Capture（Discord/Chrome が要求する形）へ切り替わる。
  よって **Client を開始してから Discord を開く**順序が必須。

## LANCast（このアプリ）を作る判断

手動 ffmpeg 運用を、解像度/FPS/ビットレート/エンコーダ/バッファ/カスタム引数を
GUI で変えられ、Host/Client どちらにもなれるアプリに置き換える。要件:

- 実行環境に追加依存を持ち込まない（単一バイナリ）。開発依存の導入は可。
- 依存不足時は導入手順 UI を出し主機能を無効化（自動実行しない）。
- 軽量優先。自動テストで品質担保。UI はシンプル優先。

## GUI フレームワーク選定: giu → Gio（重要な転換）

- 当初ユーザー希望もあり **giu (Dear ImGui / cgo)** を採用し、コア＋UI を実装・動作するところまで作った。
- しかし **macOS 26 (Tahoe) で giu バイナリが起動時クラッシュ**:
  `Assertion failed: g.PlatformIO.Monitors.Size > 0`（cimgui-go v1.5.0 / GLFW がモニタを列挙できない）。
  Bash 直起動・Terminal 起動の両方で再現＝実行コンテキストではなくフレームワーク側の非互換と判定。
- ユーザー指示「要件を満たさない場合は他を使用」に従い **Gio へ移植**。
  Gio は同条件で正常起動（macOS で視覚確認、Ubuntu デスクトップ DISPLAY=:0 でも起動確認）。
- コアパッケージ（config/ffmpeg/deps/runner）はフレームワーク非依存に作っていたため、
  差し替えは `internal/ui` と `main.go` のみで済んだ。バイナリは giu 16MB → Gio 11MB。

## レビューと最適化

- 実装後、第三者レビューをサブエージェント2観点（Go 正しさ・並行性 / UX・パイプライン意味論）で実施。
  反映した主な修正:
  - 数値フィールド空欄→0 で無効引数のまま起動するのをバリデーションで防止（`config.*.Validate`）。
  - v4l2 権限チェックを `open(O_WRONLY)` から `unix.Access` に変更（open 副作用回避）。
  - 再チェックボタンの `||` 短絡で Clicked イベント消費が漏れる問題を個別評価に修正。
  - Host 出力 UDP に `pkt_size=1316` を付与（断片化回避）。
  - Stop をプロセスグループ送信＋cmd 同一性チェックに（リーク耐性）。
  - v4l2loopback の導入コマンドをデバイス番号に整合させ、Discord 非表示時の exclusive_caps 注記を追加。
  - UI: 幅×高さを1行化、エンコーダに位置表示、キャプチャ入力と画面収録許可の補足。
- `/simplify`（4観点並列レビュー）で `strconv` 統一・`slices.Index`・プリセットのループ化・
  deps の共通化を適用。Altitude は問題なしと評価。

## ビルド/配置

- 開発機 = MacBook Neo（`~/develop/lancast/`）。Linux ビルドは SSH 先 `i7-7700HQ.ud` でネイティブ実行。
- cgo のためクロスコンパイルはせず、各 OS でネイティブビルドする方針。
- Windows は「想定はするが動作は非要件」。必要になったら別セッションで拡張。

## 受信側の堅牢化（再接続・解像度変更・どちらから開始してもOK）

症状: 初回は映るが、片側を停止して再開すると Discord がクラッシュ/解像度ズレ。原因は
**仮想カメラのフォーマットが最初の接続時に決まり、再接続や解像度変更で食い違う**こと、
および**送出が途切れると writer(ffmpeg) が落ちてカメラ自体が消える**こと。

採用した設計（`internal/runner/supervisor.go` の `ClientSupervisor`）:

- **出力モード fixed/follow**（`config.ClientConfig.OutputMode` + `CamWidth/CamHeight`、既定 fixed/1920x1080）。
  - fixed: 受信を常に Cam 解像度へ `scale+pad`（`ffmpeg.fixedVF`）。入力解像度が何でも v4l2 の
    フォーマットは一定 → 再接続・ホスト解像度変更で Discord が落ちない。**これが根治**。
  - follow: 従来どおりホスト解像度に追従（`followVF`）。原理上カメラを開き直すため不安定。
- **待機⇄ライブの状態機械**: 開始直後やホスト停止中は **待機映像**（`ClientPlaceholderArgs`＝lavfi color、
  ライブと同一フォーマット）を v4l2 へ流し続け、仮想カメラを生かす。UDP 監視で到着検出→ライブへ、
  `frame=` の停滞(5s)/ffmpeg 終了→待機へ。これで「Client 開始後は Discord をいつ開いてもよい」
  「片側ターミネートでも自動再接続」「ホスト解像度変更も stall→再接続で吸収」を満たす。
- **ホスト設定の自動貼り直し**: 稼働中に解像度等を変えると送出 ffmpeg をデバウンス(700ms)後に
  貼り直す（`ui.maybeReapplyHost` + 直列ワーカー `hostRestartWorker`。UI スレッドを止めない）。
- v4l2loopback git 版 `9ef83fb` は `keep_format`/timeout 系 module param が無い。よって driver 任せの
  フォーマット固定は使わず、**全 writer が同一フォーマットで書く**ことで一定化する方針（AGENTS.md の
  「依存を自動実行しない」にも沿う＝modprobe/v4l2loopback-ctl を自動実行しない）。
- 注: `exclusive_caps=1` のため、Discord は writer 接続（=Client 開始）より前にはカメラを見られない。
  Host/Client の起動順は UDP のため任意。**未検証**: 実機 Discord での再接続・1080p60 通し確認は要実施。

## 関連リポジトリ

- PC ナレッジベース（`~/develop/PC`）に Ubuntu 受信機 `i7-7700HQ.ud` の v4l2loopback 恒久化が記録される想定。
