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

`LANCast` を起動 → **Client (受信)** タブ → 受信ポート/出力デバイスを確認 → **開始**。
（Discord は **開始後に** 開き、カメラ「MacScreen」を選択。`exclusive_caps=1` の仕様上、writer 接続後にしか仮想カメラとして見えない）

### 3. Host を起動（送信側 Mac）

`LANCast` を起動 → **Host (送信)** タブ → 送信先 IP を受信側に設定 → **開始**。

各タブの「実行コマンド」欄に実際の ffmpeg コマンドが表示される（コピー可）。不足依存があれば「依存 / Setup」タブに導入コマンドが出る。

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
| `-bitrate` / `-fps` / `-size` | host | ビットレート kbps / FPS / `1280x720` |
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

# テスト
go test -race ./...
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
