# LANCast

LAN 内で PC の画面を別 PC へ送り、受信側を**仮想カメラ**として見せる、ffmpeg ラッパー GUI。
主用途は **MacBook の画面 → Ubuntu の v4l2loopback 仮想カメラ → Discord で画面配信**。

- **単一バイナリ / 追加ランタイム依存なし**（OS 標準の GPU・X11/Wayland のみ使用。ffmpeg 等の外部ツールは検出して導入手順を表示）
- **Host(送信) / Client(受信) を同一インスタンスで切替・同時起動可**（1:1 前提）
- 解像度・FPS・ビットレート・エンコーダ・バッファ・カスタム引数を GUI で変更
- 依存（ffmpeg / v4l2loopback）を検出し、不足時は導入コマンドを表示して**開始ボタンを無効化**（自動実行はしない）
- GUI フレームワーク: [Gio](https://gioui.org)（軽量・イミディエイトモード）

> giu (Dear ImGui) を当初採用したが、macOS 26 (Tahoe) で GLFW のモニタ列挙アサートにより起動不可だったため Gio に変更した。詳細は [markdown/main-memory.md](markdown/main-memory.md)。

## 動作要件

| | Host (送信) | Client (受信) |
|---|---|---|
| macOS | ✅ avfoundation + videotoolbox | ❌ (v4l2loopback 非対応) |
| Linux | ✅ x11grab | ✅ v4l2loopback |
| Windows | 🔶 未検証 (gdigrab/nvenc 想定) | ❌ |

外部依存: `ffmpeg`、（Client のみ）`v4l2loopback` カーネルモジュール。

## 使い方

### 1. 受信側 (Ubuntu) で v4l2loopback を準備

```bash
sudo modprobe v4l2loopback devices=1 video_nr=10 card_label=MacScreen exclusive_caps=1
```

> kernel 6.17+ では apt 版 (0.12.x) が ffmpeg と非互換。git 版 (0.15+) を DKMS 導入すること。

### 2. Client を起動（受信側 Ubuntu）

`LANCast` を起動 → **Client (受信)** タブ → 受信ポート/出力デバイス/**出力モード**を確認 → **開始**。
Client は開始した瞬間から**待機映像**で仮想カメラを生かし続けるので、**Discord は Client 開始後ならいつでも**開いてカメラ「MacScreen」を選択できる（`exclusive_caps=1` の仕様上、Client 開始＝writer 接続より前には仮想カメラとして見えない）。Host の送出を検出すると自動で映像へ切り替わり、Host を止めると待機映像へ戻る（再開で自動復帰）。

**出力モード**:
- **fixed（固定スケール・既定）**: 受信映像をカメラ解像度（既定 1920×1080）へスケール/黒帯で収める。ホスト解像度を変えても・再接続しても仮想カメラのフォーマットは不変で **Discord が落ちない**。下位の解像度/FPS は Discord 側で選んで配信できる。
- **follow（ホスト追従）**: カメラ解像度をホスト送出に一致させる。歪みは無いが、解像度変更・再接続のたびにカメラを開き直すため不安定になりうる。

### 3. Host を起動（送信側 Mac）

`LANCast` を起動 → **Host (送信)** タブ → 送信先 IP を受信側に設定 → **開始**。Host と Client は**どちらを先に起動してもよい**（UDP 送出のため順序非依存）。
稼働中に解像度・FPS・ビットレート等を変えると、**送出を自動で貼り直して即反映**する（fixed モードの Client 側は自動で追従する）。

> **macOS 初回のみ**: 「システム設定 > プライバシーとセキュリティ > 画面収録」で **LANCast**（コマンド実行時はターミナル）を許可し、アプリを再起動すること。未許可だと黒画面/失敗になる。Host タブの「画面収録を許可（システム設定を開く）」ボタンから直接開ける。

各タブの「実行コマンド」欄に実際の ffmpeg コマンドが表示される（コピー可）。不足依存があれば「依存 / Setup」タブに導入コマンドが出る。

> 本書中の IP・ホスト名（`192.168.0.215` / `i7-7700HQ.ud` 等）は筆者環境の例。自分の環境に読み替えること（受信側 Ubuntu の IP は `hostname -I` で確認）。

## トラブルシューティング

| 症状 | 原因 / 対処 |
|---|---|
| Discord のカメラ一覧に「MacScreen」が出ない | **受信(Client)を先に開始**してから Discord を開く（`exclusive_caps=1` のため writer 接続後にのみ Capture 化）。出ない場合は `sudo modprobe -r v4l2loopback` 後に `exclusive_caps` を付け外しして再ロード。Discord 再起動も有効 |
| `VIDIOC_G_FMT: Invalid argument`（受信側） | apt 版 v4l2loopback 0.12.x が kernel 6.17 と非互換。git 版 0.15+ を DKMS 導入 |
| `Address already in use`（受信側） | 前回の ffmpeg がポートを掴んだまま。`lsof -iUDP:5004` で確認し残プロセスを終了（`pkill -f 'ffmpeg.*5004'`） |
| Discord で 1080p 60fps GoLive を開始すると落ちる | 仮想カメラの提示フレームレートが不定だと Chromium のキャプチャ経路が高 fps で破綻し得る。Client タブの **FPS** をホストの FPS に合わせて固定し（`0=ソースのまま`、CLI は `-fps`）CFR で提示する。ホスト 60fps なら Client も 60 にする |
| 一度停止して再開すると Discord が落ちる / 解像度がズレる | 受信を **fixed（固定スケール）モード**にする（既定）。受信映像を一定のカメラ解像度へ収めるため、再接続やホスト解像度変更でも仮想カメラのフォーマットが変わらず落ちない。follow（ホスト追従）モードは原理上カメラを開き直すため不安定になりうる |
| ホストを止めるとカメラ映像が固まる/消える | Client は待機映像で仮想カメラを生かし続ける（数秒のフレーム停滞でホスト停止と判断し待機へ）。ホストを再開すれば自動でライブへ復帰する。Discord 側はカメラを選び直さなくてよい |
| Mac で黒画面/`Configuration of video device failed` | 画面収録の許可が未設定。手順3の許可後にアプリ再起動 |
| ffmpeg が「見つかりません」 | `brew install ffmpeg`（mac）/ `sudo apt install ffmpeg`（Linux）。GUI 版は Homebrew パスも自動探索する |

## インストール / アップデート

アプリ一覧（mac=Launchpad/Spotlight、Ubuntu=GNOME アプリグリッド）から起動できるように
インストールする。**アップデートは同じコマンドを再実行するだけ**（最新ソースから再ビルドして上書き）。

```bash
./scripts/install.sh
```

- macOS: `/Applications/LANCast.app`（カスタムアイコン付き）
- Ubuntu (`i7-7700HQ.ud`): `~/.local/bin/lancast` + `~/.local/share/applications/lancast.desktop`

mac で実行すると、ローカル(mac)と SSH 先の Linux の**両方**へインストールする。
インストール先 Linux ホストは環境変数 `LINUX_SSH` で変更可（空にすると remote をスキップ）。

## CLI / ヘッドレス実行

引数なしで GUI 起動。`-host` / `-client` で **GUI なし・即時開始**（SSH 越しの自動化・常駐・統合テスト向け）。
ffmpeg のログ（`frame=` 進捗など）を標準出力へ流し、`Ctrl-C`(SIGINT) で graceful 停止する。
依存が不足する場合は導入コマンドを表示して中止する（自動実行はしない）。

```bash
# 受信側(Ubuntu): 仮想カメラへ書き込み
lancast -client -port 5004 -device /dev/video10 -debug

# 送信側(Mac): 画面を送出
lancast -host -dest 192.168.0.215 -port 5004 -debug
```

保存済み設定をベースに、以下のフラグで上書きできる（指定したものだけ反映）:

| フラグ | 対象 | 説明 |
|---|---|---|
| `-host` / `-client` | — | ヘッドレスで該当モードを即時起動 |
| `-debug` | — | 詳細ログ（設定内容・依存チェック）を出力 |
| `-dest` | host | 送信先 IP |
| `-port` | 両方 | ポート（host=送信先 / client=受信） |
| `-source` | host | キャプチャ入力（例 `3:none`, `:0.0`） |
| `-encoder` | host | エンコーダ |
| `-bitrate` / `-size` | host | ビットレート kbps / 解像度 `1280x720` |
| `-fps` | 両方 | host=送出 FPS / client=仮想カメラへ提示する固定 FPS（`0`=ソースのまま） |
| `-device` | client | 出力デバイス（例 `/dev/video10`） |
| `-fifo` | client | 受信バッファ |
| `-extra` | 両方 | ffmpeg 追加引数 |

> 検証: Mac 画面 → `lancast -host` → UDP → `lancast -client` → `/dev/video10` を
> `ffmpeg -f v4l2 -i /dev/video10` でキャプチャし、実映像が流れることを確認済み。

## ビルド / 起動（開発）

```bash
# 開発実行
go run .

# 自分の OS 向けバイナリ
go build -o bin/lancast .

# 単体テスト
go test -race ./...

# E2E（実機: Mac→Linux→/dev/video10 を通し、仮想カメラから1フレーム取得して検証）
./scripts/e2e.sh                          # 既定 720p
./scripts/e2e.sh --size 1920x1080 --bitrate 15000
```

cgo を使うため**クロスコンパイルは避け、各 OS 上でネイティブビルド**する。
複数 OS 向けの一括ビルドは [scripts/build.sh](scripts/build.sh) を参照（mac はローカル、Linux は SSH 先でビルド）。

## 構成

```
main.go                      エントリポイント（Gio ウィンドウ起動）
internal/config/             設定スキーマ・OS別デフォルト・保存/読込・バリデーション
internal/ffmpeg/             設定 → ffmpeg 引数生成（純関数）
internal/deps/               ffmpeg/エンコーダ/v4l2loopback の検出
internal/runner/             ffmpeg プロセスのライフサイクル・ログ収集
internal/ui/                 Gio GUI（薄い層）
```

コアロジック（config/ffmpeg/deps/runner）はフレームワーク非依存の純パッケージで、
自動テストで品質を担保している。GUI は薄く保つ方針。詳細な規約は [AGENTS.md](AGENTS.md)。
