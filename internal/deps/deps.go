// Package deps は実行環境の依存（ffmpeg・エンコーダ・v4l2loopback）を検出する。
package deps

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"lancast/internal/config"
)

// Check は単一の依存チェック結果。
type Check struct {
	Name   string // 表示名
	OK     bool   // 充足しているか
	Detail string // 状況説明
	Fix    string // 解消用コマンド（空なら無し）
}

// Result は1モードぶんのチェック集合。
type Result struct {
	Checks []Check
}

// OK は全チェックが充足していれば true。
func (r Result) OK() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}
	return true
}

// ---- パース純関数（テスト対象）----

// ParseEncoders は `ffmpeg -encoders` の出力からエンコーダ名集合を抽出する。
func ParseEncoders(out string) map[string]bool {
	res := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		flags := fields[0]
		// フラグ列は [A-Z.] のみ（例: "V....D"）。桁数は ffmpeg のバージョンで
		// 変わりうるため下限のみ確認する。
		if len(flags) < 6 || !isFlagToken(flags) {
			continue
		}
		// 凡例行（" V..... = Video"）や区切りを除外し、エンコーダ名のみ採用。
		if !isEncoderName(fields[1]) {
			continue
		}
		res[fields[1]] = true
	}
	return res
}

// isEncoderName はエンコーダ名として妥当か（識別子文字のみ）判定する。
func isEncoderName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func isFlagToken(s string) bool {
	for _, r := range s {
		if r != '.' && !(r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// ParseModuleLoaded は /proc/modules 相当の内容から module がロード済みか判定する。
func ParseModuleLoaded(procModules, name string) bool {
	for _, line := range strings.Split(procModules, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == name {
			return true
		}
	}
	return false
}

// ---- 実環境アクセス ----

// FFmpegPath は ffmpeg の絶対パスと存在可否を返す。
//
// macOS の GUI アプリは launchd の最小 PATH で起動され Homebrew の
// /opt/homebrew/bin を含まないため、LookPath が失敗しても一般的な
// インストール先を直接探索する。
func FFmpegPath() (string, bool) {
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, true
	}
	for _, p := range commonFFmpegPaths() {
		if isExecutable(p) {
			return p, true
		}
	}
	return "", false
}

// commonFFmpegPaths は OS 別の ffmpeg 既定インストール先候補を返す。
func commonFFmpegPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/opt/homebrew/bin/ffmpeg", // Apple Silicon Homebrew
			"/usr/local/bin/ffmpeg",    // Intel Homebrew
			"/opt/local/bin/ffmpeg",    // MacPorts
			"/usr/bin/ffmpeg",
		}
	case "linux":
		return []string{
			"/usr/bin/ffmpeg",
			"/usr/local/bin/ffmpeg",
			"/snap/bin/ffmpeg",
		}
	case "windows":
		return []string{
			`C:\Program Files\ffmpeg\bin\ffmpeg.exe`,
			`C:\ffmpeg\bin\ffmpeg.exe`,
		}
	default:
		return nil
	}
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}

func ffmpegInstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install ffmpeg"
	case "linux":
		return "sudo apt install ffmpeg"
	case "windows":
		return "winget install Gyan.FFmpeg"
	default:
		return "install ffmpeg"
	}
}

// commonEncoders は host モードで一般的な画面エンコーダ候補（提示用）。
var commonEncoders = []string{
	"hevc_videotoolbox", "h264_videotoolbox",
	"h264_nvenc", "hevc_nvenc",
	"libx264", "libx265", "h264_vaapi",
}

// availableFrom は candidates のうち avail に存在するものだけを返す。
func availableFrom(candidates []string, avail map[string]bool) []string {
	var out []string
	for _, c := range candidates {
		if avail[c] {
			out = append(out, c)
		}
	}
	return out
}

var (
	encMu    sync.Mutex
	encCache map[string]bool
)

// availableEncoders は ffmpeg を実行してエンコーダ集合を返す。ffmpeg 不在時は空。
// `ffmpeg -encoders` は数百ms かかるため結果をキャッシュする（UI スレッドのブロック回避）。
// 取得に成功（非空）するまではキャッシュせず、再チェックのたびに再試行する。
func availableEncoders() map[string]bool {
	encMu.Lock()
	defer encMu.Unlock()
	if encCache != nil {
		return encCache
	}
	bin, ok := FFmpegPath()
	if !ok {
		return map[string]bool{}
	}
	out, err := exec.Command(bin, "-hide_banner", "-encoders").CombinedOutput()
	if err != nil {
		return map[string]bool{}
	}
	m := ParseEncoders(string(out))
	if len(m) > 0 {
		encCache = m
	}
	return m
}

func moduleLoaded(name string) bool {
	b, err := os.ReadFile("/proc/modules")
	if err != nil {
		return false
	}
	return ParseModuleLoaded(string(b), name)
}

// CheckHost は送信側に必要な依存を検証する。
func CheckHost(c config.HostConfig) Result {
	ffCheck, ffok := ffmpegCheck()
	checks := []Check{ffCheck}

	if ffok {
		encs := availableEncoders()
		has := encs[c.Encoder]
		fix := ""
		if !has {
			if avail := availableFrom(commonEncoders, encs); len(avail) > 0 {
				fix = "Host タブの『エンコーダ』を次のいずれかに変更してください: " + strings.Join(avail, ", ")
			} else {
				fix = "対応エンコーダを含む ffmpeg をインストールしてください"
			}
		}
		checks = append(checks, Check{
			Name:   "encoder: " + c.Encoder,
			OK:     has,
			Detail: okText(has, "利用可能（映像エンコードに使用）", "この ffmpeg では非対応"),
			Fix:    fix,
		})
	}

	if runtime.GOOS == "darwin" {
		// 厳密な許可状態は判定困難なため、情報表示（常に OK）として注意を促す。
		checks = append(checks, Check{
			Name:   "画面収録の許可 (macOS)",
			OK:     true,
			Detail: "初回は システム設定>プライバシーとセキュリティ>画面収録 で許可が必要（許可後アプリ再起動）。アプリ版とコマンド実行で許可は別管理。",
		})
	}

	return Result{Checks: checks}
}

// ffmpegCheck は ffmpeg の有無を検査する共通チェック。
func ffmpegCheck() (Check, bool) {
	bin, ok := FFmpegPath()
	return Check{
		Name:   "ffmpeg",
		OK:     ok,
		Detail: detailOrMissing(bin, ok),
		Fix:    fixIf(!ok, ffmpegInstallHint()),
	}, ok
}

// CheckClient は受信側（v4l2 仮想カメラ書き込み）に必要な依存を検証する。
func CheckClient(c config.ClientConfig) Result {
	ffCheck, _ := ffmpegCheck()
	checks := []Check{ffCheck}

	if runtime.GOOS != "linux" {
		checks = append(checks, Check{
			Name:   "v4l2loopback",
			OK:     false,
			Detail: "v4l2loopback は Linux 専用です。この PC は送信(Host)専用で、受信(Discord に映す側)は Ubuntu 機で行ってください。",
			Fix:    "", // この OS では解消不能なため修正コマンドは出さない
		})
		return Result{Checks: checks}
	}

	dev := c.OutputDevice
	nr := deviceNr(dev)
	// 同一の modprobe コマンドを module 未ロード・device 不在の両方に提示する。
	modprobeCmd := "sudo modprobe v4l2loopback devices=1 video_nr=" + nr + " card_label=MacScreen exclusive_caps=1"
	loaded := moduleLoaded("v4l2loopback")
	checks = append(checks, Check{
		Name: "v4l2loopback module",
		OK:   loaded,
		Detail: okText(loaded, "ロード済み（受信映像を仮想カメラとして見せるために必要）",
			"未ロード。未インストールなら kernel 6.17+ は git 0.15+ を DKMS 導入。ロード後も Discord/Chrome に出ない時は exclusive_caps を付け外しして再ロード。"),
		Fix: fixIf(!loaded, modprobeCmd),
	})

	_, statErr := os.Stat(dev)
	devExists := statErr == nil
	checks = append(checks, Check{
		Name:   "device: " + dev,
		OK:     devExists,
		Detail: okText(devExists, "存在します", "存在しません"),
		Fix:    fixIf(!devExists, modprobeCmd),
	})

	if devExists {
		writable := canWrite(dev)
		checks = append(checks, Check{
			Name:   "write permission",
			OK:     writable,
			Detail: okText(writable, "書き込み可能", "書き込み権限がありません"),
			Fix:    fixIf(!writable, "sudo usermod -aG video $USER して再ログイン（または sudo chgrp video "+dev+" && sudo chmod 660 "+dev+"）"),
		})
	}

	return Result{Checks: checks}
}

// deviceNr は "/dev/video10" から "10" を取り出す（数字で取れなければ "10"）。
func deviceNr(dev string) string {
	const p = "/dev/video"
	if strings.HasPrefix(dev, p) {
		if n := dev[len(p):]; isAllDigits(n) {
			return n
		}
	}
	return "10"
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ---- 表示ヘルパ ----

func detailOrMissing(path string, ok bool) string {
	if ok {
		return path
	}
	return "見つかりません"
}

// okText は ok の真偽で yes/no いずれかの文字列を返す。
func okText(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func fixIf(cond bool, fix string) string {
	if cond {
		return fix
	}
	return ""
}

// Summary は結果を1行に要約する（ログ用）。
func (r Result) Summary() string {
	n := 0
	for _, c := range r.Checks {
		if c.OK {
			n++
		}
	}
	return fmt.Sprintf("%d/%d OK", n, len(r.Checks))
}
