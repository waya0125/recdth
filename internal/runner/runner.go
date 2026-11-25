// Package runner は dth（と外部 B25 復号コマンド）をパイプでつないで動かし、TS を出力先へ書く。
//
// 子プロセスはすべて 1 つのプロセスグループに入れ、次のどの終わり方でもグループごと止めて
// Wait で回収する（ゾンビやチューナーの占有を残さない）。
//
//   - 録画時間に達した / SIGINT・SIGTERM・SIGHUP（ctx の取り消し）:
//     先頭の dth にだけ SIGTERM を送り、後段の B25 には EOF で残りを書き出させる。
//     stop_grace 秒たっても終わらなければグループに SIGKILL。
//   - 出力先が閉じた（EPIPE など書き込みエラー）: グループ全体に SIGTERM、猶予後 SIGKILL。
//   - 最初の TS が届かない（first_byte_timeout）: 同上で、エラーとして返す。
//
// Linux では Pdeathsig も設定し、recdth 自体が SIGKILL されても子に SIGTERM が届くようにする。
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/waya0125/recdth/internal/logx"
)

// ErrOutputClosed は出力先（下流のパイプ）が閉じたことを表す。録画サーバーが止めた正常系として扱える。
var ErrOutputClosed = errors.New("出力先が閉じられました")

// ErrNoData は first_byte_timeout までに TS が届かなかったことを表す。
var ErrNoData = errors.New("TS が届きません")

// Config は 1 回の実行内容。
type Config struct {
	// Stages はパイプでつなぐコマンド列。Stages[0] が dth。
	Stages [][]string
	// Out は TS の出力先。
	Out io.Writer
	// Stderr は子プロセスの stderr（通常は os.Stderr）。
	Stderr io.Writer
	// Limit は最初の TS を受けてから止めるまでの時間（0 = 無制限）。
	Limit time.Duration
	// FirstByteTimeout は起動から最初の TS までの上限（0 = 無効）。
	FirstByteTimeout time.Duration
	// StopGrace は SIGTERM から SIGKILL までの猶予。
	StopGrace time.Duration
	// Log は stderr へのロガー。
	Log *logx.Logger
}

// Result は実行結果。
type Result struct {
	// Bytes は出力先に書いたバイト数。
	Bytes int64
	// Reason は止まった理由（"limit" / "signal" / "eof" / "output-closed" / "no-data"）。
	Reason string
}

const bufSize = 188 * 1024

type stopKind int

const (
	stopNone     stopKind = iota
	stopGraceful          // 先頭だけ SIGTERM、後段は EOF で流し切る
	stopAll               // グループ全体に SIGTERM
)

// Run は Stages を起動し、終わるまで TS を Out へ書く。
func Run(ctx context.Context, c Config) (Result, error) {
	var res Result
	if len(c.Stages) == 0 || len(c.Stages[0]) == 0 {
		return res, errors.New("実行するコマンドがありません")
	}
	if c.Stderr == nil {
		c.Stderr = os.Stderr
	}
	if c.StopGrace <= 0 {
		c.StopGrace = 5 * time.Second
	}

	cmds, out, err := start(c)
	if err != nil {
		return res, err
	}
	pgid := cmds[0].Process.Pid
	c.Log.Debugf("プロセスグループ %d で起動しました", pgid)

	// 読み取り→書き込み
	firstByte := make(chan struct{})
	copyDone := make(chan error, 1)
	var written int64
	var wmu sync.Mutex
	go func() {
		buf := make([]byte, bufSize)
		first := true
		for {
			n, rerr := out.Read(buf)
			if n > 0 {
				if first {
					first = false
					close(firstByte)
				}
				if _, werr := c.Out.Write(buf[:n]); werr != nil {
					copyDone <- fmt.Errorf("%w: %v", ErrOutputClosed, werr)
					return
				}
				wmu.Lock()
				written += int64(n)
				wmu.Unlock()
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) {
					copyDone <- nil
				} else {
					copyDone <- rerr
				}
				return
			}
		}
	}()

	waitDone := make(chan struct{})
	waitErrs := make([]error, len(cmds))
	go func() {
		// 出力側を読み切ってから Wait する（StdoutPipe と違い自前の os.Pipe なので順序の制約はないが、取りこぼしを防ぐ）
		var wg sync.WaitGroup
		for i, cmd := range cmds {
			wg.Add(1)
			go func(i int, cmd *exec.Cmd) {
				defer wg.Done()
				waitErrs[i] = cmd.Wait()
			}(i, cmd)
		}
		wg.Wait()
		close(waitDone)
	}()

	var firstTimer, limitTimer <-chan time.Time
	if c.FirstByteTimeout > 0 {
		t := time.NewTimer(c.FirstByteTimeout)
		defer t.Stop()
		firstTimer = t.C
	}

	stopped := stopNone
	var killTimer <-chan time.Time
	var copyErr error
	copyFinished := false
	// alive は子がまだ回収されていなければ true（回収後のグループ ID 再利用に誤って送らないため）
	alive := func() bool {
		select {
		case <-waitDone:
			return false
		default:
			return true
		}
	}
	requestStop := func(kind stopKind, reason string) {
		if !alive() {
			if res.Reason == "" {
				res.Reason = reason
			}
			return
		}
		if stopped != stopNone {
			if kind == stopAll && stopped == stopGraceful {
				signalGroup(pgid, syscall.SIGTERM)
				stopped = stopAll
			}
			return
		}
		stopped = kind
		if res.Reason == "" {
			res.Reason = reason
		}
		if kind == stopGraceful {
			c.Log.Debugf("%s: dth に SIGTERM を送ります", reason)
			_ = cmds[0].Process.Signal(syscall.SIGTERM)
		} else {
			c.Log.Debugf("%s: プロセスグループに SIGTERM を送ります", reason)
			signalGroup(pgid, syscall.SIGTERM)
		}
		t := time.NewTimer(c.StopGrace)
		killTimer = t.C
	}

	ctxDone := ctx.Done()
	fb := (<-chan struct{})(firstByte)
loop:
	for {
		select {
		case <-ctxDone:
			ctxDone = nil
			c.Log.Infof("シグナルを受けたので止めます")
			requestStop(stopGraceful, "signal")
		case <-fb:
			fb = nil
			firstTimer = nil
			c.Log.Infof("TS の受信を始めました")
			if c.Limit > 0 {
				t := time.NewTimer(c.Limit)
				defer t.Stop()
				limitTimer = t.C
			}
		case <-firstTimer:
			firstTimer = nil
			c.Log.Errorf("%v 以内に TS が届きませんでした", c.FirstByteTimeout)
			requestStop(stopAll, "no-data")
		case <-limitTimer:
			limitTimer = nil
			c.Log.Infof("録画時間に達しました")
			requestStop(stopGraceful, "limit")
		case err := <-copyDone:
			copyFinished = true
			copyErr = err
			if errors.Is(err, ErrOutputClosed) {
				c.Log.Infof("出力先が閉じられたので止めます")
				res.Reason = "output-closed"
				requestStop(stopAll, "output-closed")
			} else if res.Reason == "" {
				res.Reason = "eof"
			}
			// 最終段が終わった。前段が残っていれば止める
			requestStop(stopAll, res.Reason)
		case <-killTimer:
			killTimer = nil
			if alive() {
				c.Log.Warnf("子プロセスが %v 以内に終わらないので SIGKILL します", c.StopGrace)
				signalGroup(pgid, syscall.SIGKILL)
			}
		case <-waitDone:
			break loop
		}
	}
	// 子がすべて終わったので出力側のパイプを読み切る（残りがあれば書き出す）
	if !copyFinished {
		copyErr = <-copyDone
		if errors.Is(copyErr, ErrOutputClosed) && res.Reason == "" {
			res.Reason = "output-closed"
		}
	}
	out.Close()
	wmu.Lock()
	res.Bytes = written
	wmu.Unlock()
	if res.Reason == "" {
		res.Reason = "eof"
	}

	switch res.Reason {
	case "limit", "signal":
		return res, nil
	case "output-closed":
		return res, ErrOutputClosed
	case "no-data":
		return res, ErrNoData
	}
	// 子が自分で終わった（dth tune の -t 到達、またはエラー）
	if copyErr != nil && !errors.Is(copyErr, ErrOutputClosed) {
		return res, fmt.Errorf("TS の読み取りに失敗しました: %w", copyErr)
	}
	for i, err := range waitErrs {
		if err != nil {
			return res, fmt.Errorf("%s が異常終了しました: %w", stageName(c.Stages[i]), err)
		}
	}
	return res, nil
}

// start は Stages を os.Pipe でつないで起動し、最終段の出力の読み口を返す。
func start(c Config) ([]*exec.Cmd, *os.File, error) {
	var cmds []*exec.Cmd
	cleanup := func() {
		if len(cmds) == 0 {
			return
		}
		signalGroup(cmds[0].Process.Pid, syscall.SIGKILL)
		for _, cmd := range cmds {
			_ = cmd.Wait()
		}
	}
	var prev *os.File
	for i, argv := range c.Stages {
		if len(argv) == 0 {
			cleanup()
			return nil, nil, fmt.Errorf("%d 番目のコマンドが空です", i+1)
		}
		r, w, err := os.Pipe()
		if err != nil {
			if prev != nil {
				prev.Close()
			}
			cleanup()
			return nil, nil, err
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdout = w
		cmd.Stderr = c.Stderr
		if prev != nil {
			cmd.Stdin = prev
		}
		pgid := 0
		if len(cmds) > 0 {
			pgid = cmds[0].Process.Pid
		}
		setProcAttr(cmd, pgid)
		c.Log.Debugf("起動: %s", strings.Join(argv, " "))
		err = cmd.Start()
		w.Close()
		if prev != nil {
			prev.Close()
		}
		if err != nil {
			r.Close()
			cleanup()
			return nil, nil, fmt.Errorf("%s を起動できません: %w", stageName(argv), err)
		}
		cmds = append(cmds, cmd)
		prev = r
	}
	return cmds, prev, nil
}

func stageName(argv []string) string {
	if len(argv) == 0 {
		return "(空)"
	}
	return argv[0]
}
