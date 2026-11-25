//go:build linux

package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/waya0125/recdth/internal/logx"
)

// 偽の dth: 188 バイトの TS パケットもどきを書き続け、自分の PID をファイルに残す。
const fakeTS = `echo $$ >> "$PIDS"; while :; do printf 'G%0187d' 0; done`

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) Len() int                    { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Len() }

func setup(t *testing.T) string {
	t.Helper()
	pids := filepath.Join(t.TempDir(), "pids")
	t.Setenv("PIDS", pids)
	return pids
}

// assertGone は記録された PID がすべて消えている（ゾンビも残っていない）ことを確かめる。
func assertGone(t *testing.T, pidFile string, want int) {
	t.Helper()
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(b))
	if len(fields) < want {
		t.Fatalf("PID が %d 個しか記録されていません: %q", len(fields), fields)
	}
	for _, f := range fields {
		pid, _ := strconv.Atoi(f)
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Errorf("PID %d がまだ残っています (%v)", pid, err)
		}
	}
}

func cfg(out io.Writer, stages ...[]string) Config {
	return Config{
		Stages:    stages,
		Out:       out,
		Stderr:    io.Discard,
		StopGrace: 2 * time.Second,
		Log:       logx.New(io.Discard, 0),
	}
}

func sh(script string) []string { return []string{"/bin/sh", "-c", script} }

func TestLimit(t *testing.T) {
	pids := setup(t)
	var out syncBuf
	c := cfg(&out, sh(fakeTS))
	c.Limit = 300 * time.Millisecond
	start := time.Now()
	res, err := Run(context.Background(), c)
	if err != nil || res.Reason != "limit" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	// 正常なら SIGTERM で即止まる。負荷が高いと SIGKILL（StopGrace 2 秒）まで行くことがあるので余裕をみる
	if el := time.Since(start); el > 3*time.Second {
		t.Errorf("止まるまで %v かかりました", el)
	}
	if out.Len() == 0 || res.Bytes != int64(out.Len()) {
		t.Errorf("書いたバイト数 %d / 結果 %d", out.Len(), res.Bytes)
	}
	assertGone(t, pids, 1)
}

func TestPipelineFlushesB25Stage(t *testing.T) {
	pids := setup(t)
	var out syncBuf
	// 後段: 入力を全部読んでから最後に END を書く（EOF で書き出す B25 のまね）
	b25 := sh(`echo $$ >> "$PIDS"; cat >/dev/null; printf END`)
	c := cfg(&out, sh(fakeTS), b25)
	// 後段は最後にしか書かないので、ctx の取り消し（シグナル相当）で止める
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := Run(ctx, c)
	if err != nil || res.Reason != "signal" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if !strings.HasSuffix(out.b.String(), "END") {
		t.Errorf("後段の書き出しが失われました: %q", out.b.String())
	}
	assertGone(t, pids, 2)
}

func TestSignalStopsGroup(t *testing.T) {
	pids := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	res, err := Run(ctx, cfg(io.Discard, sh(fakeTS), []string{"cat"}))
	if err != nil || res.Reason != "signal" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	assertGone(t, pids, 1)
}

type brokenWriter struct{ n int }

func (b *brokenWriter) Write(p []byte) (int, error) {
	if b.n++; b.n > 3 {
		return 0, syscall.EPIPE
	}
	return len(p), nil
}

func TestOutputClosedKillsAll(t *testing.T) {
	pids := setup(t)
	// 後段は SIGTERM を無視するので SIGKILL まで行く
	b25 := sh(`trap '' TERM; echo $$ >> "$PIDS"; cat`)
	c := cfg(&brokenWriter{}, sh(fakeTS), b25)
	c.StopGrace = 300 * time.Millisecond
	res, err := Run(context.Background(), c)
	if !errors.Is(err, ErrOutputClosed) || res.Reason != "output-closed" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	assertGone(t, pids, 2)
}

func TestIgnoringSIGTERMGetsKilled(t *testing.T) {
	pids := setup(t)
	c := cfg(io.Discard, sh(`trap '' TERM; `+fakeTS))
	c.Limit = 100 * time.Millisecond
	c.StopGrace = 300 * time.Millisecond
	start := time.Now()
	res, err := Run(context.Background(), c)
	if err != nil || res.Reason != "limit" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Errorf("SIGKILL までに %v かかりました", el)
	}
	assertGone(t, pids, 1)
}

func TestNoData(t *testing.T) {
	pids := setup(t)
	c := cfg(io.Discard, sh(`echo $$ >> "$PIDS"; exec sleep 30`))
	c.FirstByteTimeout = 200 * time.Millisecond
	res, err := Run(context.Background(), c)
	if !errors.Is(err, ErrNoData) || res.Reason != "no-data" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	assertGone(t, pids, 1)
}

func TestChildExitStatus(t *testing.T) {
	var out syncBuf
	res, err := Run(context.Background(), cfg(&out, sh(`printf abc; exit 0`)))
	if err != nil || res.Reason != "eof" || out.b.String() != "abc" {
		t.Errorf("正常終了: %+v %v %q", res, err, out.b.String())
	}
	_, err = Run(context.Background(), cfg(io.Discard, sh(`echo boom >&2; exit 3`)))
	if err == nil || !strings.Contains(err.Error(), "異常終了") {
		t.Errorf("異常終了: %v", err)
	}
}

func TestStartFailure(t *testing.T) {
	pids := setup(t)
	_, err := Run(context.Background(), cfg(io.Discard, sh(fakeTS), []string{"/nonexistent/b25"}))
	if err == nil {
		t.Fatal("起動できないコマンドはエラーになるべき")
	}
	// 先に起動した dth も止まっていること
	time.Sleep(50 * time.Millisecond)
	if b, _ := os.ReadFile(pids); len(b) > 0 {
		assertGone(t, pids, 1)
	}
}
