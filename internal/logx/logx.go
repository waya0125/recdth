// Package logx は stderr だけに書く小さなロガー。stdout は TS 専用なので一切使わない。
package logx

import (
	"fmt"
	"io"
	"sync"
)

// Logger はレベル付きで書き出す。Verbose 0 = 警告とエラー、1 = 情報、2 以上 = デバッグ。
type Logger struct {
	mu      sync.Mutex
	w       io.Writer
	Verbose int
}

// New は w（通常は os.Stderr）に書くロガーを返す。
func New(w io.Writer, verbose int) *Logger { return &Logger{w: w, Verbose: verbose} }

func (l *Logger) out(level, format string, a ...any) {
	if l == nil || l.w == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "recdth: %s"+format+"\n", append([]any{level}, a...)...)
}

func (l *Logger) Errorf(format string, a ...any) { l.out("エラー: ", format, a...) }
func (l *Logger) Warnf(format string, a ...any)  { l.out("警告: ", format, a...) }

func (l *Logger) Infof(format string, a ...any) {
	if l != nil && l.Verbose >= 1 {
		l.out("", format, a...)
	}
}

func (l *Logger) Debugf(format string, a ...any) {
	if l != nil && l.Verbose >= 2 {
		l.out("debug: ", format, a...)
	}
}
