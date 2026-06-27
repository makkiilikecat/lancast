# AGENTS.md

LANCast プロジェクトで作業する AI エージェント / 開発者向けの規約。

## このプロジェクトは何か

ffmpeg をラップした GUI で、LAN 内の画面共有（Host=送信 / Client=受信）を行い、
受信側を v4l2loopback 仮想カメラとして Discord 等に見せる。背景と経緯は
[markdown/main-memory.md](markdown/main-memory.md) を最初に読むこと。

## アーキテクチャ原則

- **コアロジックとUIを分離する。** `internal/{config,ffmpeg,deps,runner}` は GUI 非依存の
  純パッケージ。`internal/ui` と `main.go` のみ Gio に依存する。新機能はまずコア側へ
  純関数として実装し、テストを書いてから UI に繋ぐ。
- **ffmpeg 引数生成は純関数**（`ffmpeg.HostArgs` / `ffmpeg.ClientArgs`）。副作用なし・テスト必須。
- **OS 分岐は一箇所に閉じる。**
  - 既定値: `config.DefaultConfigFor(goos)`
  - エンコーダ候補: `ffmpeg.EncodersForOS(goos)`
  - プロセス制御: `runner/proc_{unix,windows}.go`（build タグ）
  - 権限チェック: `deps/access_{unix,windows}.go`（build タグ）
  - 共有インフラ（config/ffmpeg のロジック本体）に OS 特殊ケースを散らさない。

## 絶対に守ること (MUST)

- **依存を自動で導入・実行しない。** ffmpeg の起動と検出用 `ffmpeg -encoders` 以外、
  `sudo`・`modprobe`・`apt` 等を**自動実行してはならない**。不足依存は GUI にコマンドを
  テキスト表示し、ユーザーに実行させる（`deps.Check*` の `Fix` フィールド）。
- **実行環境に追加ランタイム依存を持ち込まない。** 単一バイナリ + OS 標準ライブラリ
  （GL/X11/Wayland）+ 外部 ffmpeg のみ。新規 Go 依存の追加は最小限に。
- **avfoundation 入力に `-framerate` を付けない**（mac でデバイス設定失敗を誘発する。
  FPS は `-vf fps=N` で正規化）。x11grab/gdigrab には入力側 `-framerate` が必要。
- **クロスコンパイルしない。** cgo を使うため各 OS 上でネイティブビルドする
  （`scripts/build.sh`：mac ローカル / Linux は SSH 先）。

## 品質ゲート

変更後は必ず以下を通すこと:

```bash
gofmt -w internal/ main.go
go vet ./...
go test -race ./...
```

- 新しいコア機能には必ずテストを追加する（GUI は薄く保ち、ロジックをコアへ寄せてテスト可能にする）。
- GUI 変更時は実機で起動確認する（macOS 26 で giu が起動不可だった前例があるため、
  フレームワーク/描画に関わる変更は特に視覚確認する）。

## UI 方針

- **シンプル最優先。** タブは Host / Client / 依存(Setup) の3つ。1画面1目的。
- 各タブに「実行コマンド」プレビュー（コピー可能な ReadOnly エディタ）を出し、
  GUI 操作と生成コマンドの対応を常に見せる。
- 依存未充足時は開始ボタンを `gtx.Disabled()` で無効化し、警告から Setup タブへ誘導する。

## ビルド成果物

- バイナリは `bin/` に出力するが **git 管理しない**（`.gitignore` 済み）。
- macOS 配布用に `bin/LANCast.app` バンドルを生成できる（`scripts/build.sh`）。
