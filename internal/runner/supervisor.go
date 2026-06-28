package runner

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClientSupervisor は受信側の v4l2 仮想カメラ供給を継続管理する高レベル制御器。
//
// 単一の ffmpeg では「受信が途切れたら待機映像へ」「ホスト解像度が変わったら貼り直し」
// といった再接続を扱えない。そこで本器は内部の Runner を使って 2 つの状態を自動で
// 行き来する:
//
//	待機(WAITING): プレースホルダ映像を仮想カメラへ流しつつ UDP を監視する。開始直後や
//	               ホスト停止中でも仮想カメラを生かし続け、Discord がカメラを失わない。
//	ライブ(LIVE):  受信ストリームを仮想カメラへ流す。フレームが一定時間来ない（ホスト停止）か
//	               ffmpeg が終了したら待機へ戻り、再びパケットを検出したらライブへ復帰する。
//
// これにより「どちらから開始してもよい」「片側を落としても再接続できる」「稼働中の
// ホスト解像度変更に追従する」を、仮想カメラを途切れさせずに実現する。
type ClientSupervisor struct {
	r *Runner

	mu       sync.Mutex
	active   bool
	stopCh   chan struct{}
	doneCh   chan struct{}
	lastProg time.Time
	state    string

	// 受信は無加工のため、待機(プレースホルダ)映像の寸法はライブ受信の解像度に合わせる
	// 必要がある（待機⇄ライブで仮想カメラのフォーマットを一致させ、Discord 側の斜めズレを防ぐ）。
	bin           string
	liveArgs      []string
	placeholderFn func(w, h int) []string // 待機映像引数を寸法から生成する
	camW, camH    int                     // 直近に受信したホスト送出の解像度（待機映像に使う）

	// OnUpdate はログ/状態更新時に呼ばれる（UI 再描画トリガ用）。Start 前に設定する。
	OnUpdate func()
	// OnLine は新しいログ行ごとに呼ばれる（ヘッドレス時の標準出力用）。Start 前に設定する。
	OnLine func(string)
	// OnState は状態遷移（待機/ライブ/停止）通知（任意・UI/ログ表示用）。Start 前に設定する。
	OnState func(string)
	// OnFormat は受信解像度を学習して変化したとき呼ばれる（設定への永続化用・任意）。Start 前に設定する。
	OnFormat func(w, h int)
}

const (
	// supStallTimeout はライブ中にフレームが来なくなってからホスト停止とみなすまでの猶予。
	supStallTimeout = 5 * time.Second
	// supReconnectDelay は再試行の間隔（タイトループ防止）。
	supReconnectDelay = 1500 * time.Millisecond
	// supProbeInterval は UDP 監視/ライブ監視のポーリング間隔。
	supProbeInterval = 300 * time.Millisecond
	// supStopWait は内部 ffmpeg の停止を待つ上限。
	supStopWait = 4 * time.Second
)

// NewClientSupervisor は ClientSupervisor を生成する。
func NewClientSupervisor() *ClientSupervisor {
	return &ClientSupervisor{r: New()}
}

// Lines は現在のログ行のコピーを返す（UI 向け）。
func (s *ClientSupervisor) Lines() []string { return s.r.Lines() }

// Clear はログを消去する。
func (s *ClientSupervisor) Clear() { s.r.Clear() }

// Running は供給制御が稼働中か返す（待機/ライブのいずれかを回している間 true）。
func (s *ClientSupervisor) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// State は現在の状態（"待機"/"ライブ"/"停止"）を返す。
func (s *ClientSupervisor) State() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Start は供給制御を開始する。live は受信→v4l2 の引数列、placeholderFn は待機映像→v4l2 の
// 引数列を寸法(w,h)から生成する関数。camW/camH は待機映像の初期寸法（直近のホスト解像度＝
// 設定の学習値。受信が始まれば実寸へ更新される）。port は UDP 受信ポート。既に稼働中なら何もしない。
func (s *ClientSupervisor) Start(bin string, live []string, placeholderFn func(w, h int) []string, port, camW, camH int) error {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return fmt.Errorf("既に実行中です")
	}
	s.active = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.bin = bin
	s.liveArgs = live
	s.placeholderFn = placeholderFn
	if camW > 0 && camH > 0 {
		s.camW, s.camH = camW, camH
	}
	if s.camW <= 0 || s.camH <= 0 {
		s.camW, s.camH = 1280, 720 // 学習前の安全既定
	}
	s.mu.Unlock()

	s.r.OnUpdate = s.OnUpdate
	// フレーム進捗(frame=)を監視しつつ、ライブ受信の解像度を学習し、ユーザーの OnLine へ転送する。
	s.r.OnLine = func(line string) {
		if isProgressLine(line) {
			s.touch()
		}
		s.learnFormat(line)
		if s.OnLine != nil {
			s.OnLine(line)
		}
	}
	go s.loop(port)
	return nil
}

// currentPlaceholder は学習済み寸法で待機映像の引数列を生成する。
func (s *ClientSupervisor) currentPlaceholder() []string {
	s.mu.Lock()
	w, h, fn := s.camW, s.camH, s.placeholderFn
	s.mu.Unlock()
	return fn(w, h)
}

// learnFormat はライブ受信ストリームの解像度を学習し、待機映像をそれに合わせる
// （待機⇄ライブのフォーマット一致＝斜めズレ防止）。寸法が変わったら OnFormat で永続化を促す。
// 待機映像(lavfi)のストリーム行を誤学習しないよう、ライブ状態のときだけ拾う。
func (s *ClientSupervisor) learnFormat(line string) {
	s.mu.Lock()
	live := s.state == "ライブ"
	s.mu.Unlock()
	if !live {
		return
	}
	w, h, ok := parseVideoDims(line)
	if !ok {
		return
	}
	s.mu.Lock()
	changed := w != s.camW || h != s.camH
	if changed {
		s.camW, s.camH = w, h
	}
	cb := s.OnFormat
	s.mu.Unlock()
	if changed && cb != nil {
		cb(w, h)
	}
}

// videoDimsRe は ffmpeg ストリーム情報行中の解像度 "1280x720" を拾う。
var videoDimsRe = regexp.MustCompile(`(\d{2,5})x(\d{2,5})`)

// parseVideoDims は ffmpeg のストリーム情報行 "... Video: ... 1280x720 ..." から解像度を取り出す。
func parseVideoDims(line string) (w, h int, ok bool) {
	if !strings.Contains(line, "Video:") {
		return 0, 0, false
	}
	m := videoDimsRe.FindStringSubmatch(line)
	if m == nil {
		return 0, 0, false
	}
	w, _ = strconv.Atoi(m[1])
	h, _ = strconv.Atoi(m[2])
	if w < 16 || h < 16 {
		return 0, 0, false
	}
	return w, h, true
}

// Stop は供給制御を停止する（待機/ライブのどちらでも即座に終わる）。
func (s *ClientSupervisor) Stop() {
	s.mu.Lock()
	if !s.active || s.stopCh == nil {
		s.mu.Unlock()
		return
	}
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	done := s.doneCh
	s.mu.Unlock()

	s.r.Stop() // 稼働中の ffmpeg を即座に割り込む
	if done != nil {
		select {
		case <-done:
		case <-time.After(supStopWait + 2*time.Second):
		}
	}
}

// loop は待機⇄ライブの状態機械を回す。stop されるまで継続する。
func (s *ClientSupervisor) loop(port int) {
	defer close(s.doneCh)
	first := true
	for {
		if s.stopped() {
			break
		}
		if !first {
			// 失敗/切断後の再試行はタイトループにならないよう少し待つ。
			if s.waitOrStop(supReconnectDelay) {
				break
			}
		}
		first = false

		// --- 待機: プレースホルダ映像を流しつつ UDP を監視 ---
		// 待機映像の寸法は学習済みのホスト解像度（currentPlaceholder）に合わせる。
		s.setState("待機")
		if err := s.r.Start(s.bin, s.currentPlaceholder()); err != nil {
			// 出力デバイス使用中など。状態へ出して間隔を置いて再試行する。
			s.setState("待機(デバイス使用中?)")
			continue
		}
		got := s.probeUDP(port) // パケット到着か stop までブロック
		// 待機を止めてからライブへ切り替える。この間 writer が一瞬不在になるが、消費側
		// (Discord) は直前フレームを保持し、同一フォーマットで復帰するため落ちない。
		s.stopFeed()
		if !got {
			break // stop された
		}

		// --- ライブ: 受信→v4l2 ---
		// 待機映像(lavfi)も -stats で frame= を出すため lastProg が更新され得る。ライブ開始時に
		// 明示リセットし、待機由来の進捗で stall 判定が鈍らないようにする。
		s.setState("ライブ")
		s.touch()
		if err := s.r.Start(s.bin, s.liveArgs); err != nil {
			continue
		}
		s.watchLive() // 終了/停滞/stop まで監視
		s.stopFeed()
	}
	s.setState("停止")
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()
	if s.OnUpdate != nil {
		s.OnUpdate()
	}
}

// watchLive はライブ中の ffmpeg を監視し、終了・フレーム停滞・stop のいずれかで戻る。
func (s *ClientSupervisor) watchLive() {
	t := time.NewTicker(supProbeInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			if !s.r.Running() {
				return // ffmpeg 終了（ホスト消失・エラー）
			}
			if progressStalled(s.lastProgress(), time.Now(), supStallTimeout) {
				return // 一定時間フレームが来ない＝ホスト停止
			}
		}
	}
}

// probeUDP は port に UDP パケットが届くまでブロックし、届いたら true を返す。
// 返り値 false は「stop された」ことのみを意味する（呼び出し側はこれでループを終える）。
// 待機中のみ呼ぶ（ライブ中は ffmpeg がポートを占有する）。
//
// 直前ループのライブ ffmpeg がまだポートを解放しきっていないと bind が一時的に
// EADDRINUSE で失敗し得る。その場合は永久待機せず、間隔を置いて bind を再試行する
// （待機映像は流れたままなので仮想カメラは生き続け、ポートが空き次第ライブへ復帰できる）。
func (s *ClientSupervisor) probeUDP(port int) bool {
	buf := make([]byte, 2048)
	for {
		if s.stopped() {
			return false
		}
		conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: port})
		if err != nil {
			if s.waitOrStop(supReconnectDelay) {
				return false
			}
			continue // bind 再試行
		}
		got := s.readPacket(conn, buf)
		_ = conn.Close()
		if got {
			return true
		}
		// got=false は stop。ループ先頭の stopped() で抜ける。
	}
}

// readPacket は conn に最初のパケットが届けば true、stop されたら false を返す。
func (s *ClientSupervisor) readPacket(conn *net.UDPConn, buf []byte) bool {
	for {
		select {
		case <-s.stopCh:
			return false
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(supProbeInterval))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue // タイムアウト等。stop チェックして継続。
		}
		if n > 0 {
			return true
		}
	}
}

// stopFeed は内部 ffmpeg を停止し、実際に終わるまで待つ（次の Start と競合しないように）。
func (s *ClientSupervisor) stopFeed() {
	s.r.Stop()
	deadline := time.Now().Add(supStopWait)
	for s.r.Running() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}

func (s *ClientSupervisor) setState(st string) {
	s.mu.Lock()
	changed := s.state != st
	s.state = st
	s.mu.Unlock()
	if changed && s.OnState != nil {
		s.OnState(st)
	}
}

func (s *ClientSupervisor) touch() {
	s.mu.Lock()
	s.lastProg = time.Now()
	s.mu.Unlock()
}

func (s *ClientSupervisor) lastProgress() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastProg
}

func (s *ClientSupervisor) stopped() bool {
	select {
	case <-s.stopCh:
		return true
	default:
		return false
	}
}

func (s *ClientSupervisor) waitOrStop(d time.Duration) bool {
	select {
	case <-s.stopCh:
		return true
	case <-time.After(d):
		return false
	}
}

// isProgressLine は ffmpeg の進捗統計行（frame= …）かを判定する。
func isProgressLine(line string) bool {
	return strings.Contains(line, "frame=")
}

// progressStalled は最後の進捗から timeout を超えたか（＝フレーム停滞）を返す。
func progressStalled(last, now time.Time, timeout time.Duration) bool {
	return now.Sub(last) > timeout
}
