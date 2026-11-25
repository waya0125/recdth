// recdth は BUFFALO DTH10/DTH30 シリーズ用の dth コマンド（i4M1k0SU/DTH10-series-driver）を
// recpt1 / recisdb と同じ引数で使えるようにするラッパー。
//
//	recdth [オプション] <チャンネル> <秒数|-> <出力先|->
//
// 標準出力には TS 以外を一切書かない。ログ・警告・エラーはすべて標準エラー出力に書く。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/waya0125/recdth/internal/args"
	"github.com/waya0125/recdth/internal/config"
	"github.com/waya0125/recdth/internal/logx"
	"github.com/waya0125/recdth/internal/runner"
)

func main() {
	// stdout が壊れたパイプになったとき、SIGPIPE で即死せず EPIPE として受け取り、子を片付けてから終わる。
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run は recdth 本体。終了コードを返す。
func run(ctx context.Context, argv []string, stdout io.Writer, stderr io.Writer) int {
	o, err := args.ParseCLI(argv, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "recdth:", err)
		return 2
	}
	log := logx.New(stderr, o.Verbose)
	cfg, err := config.Find(o.Config)
	if err != nil {
		log.Errorf("%v", err)
		return 2
	}
	if cfg.Path != "" {
		log.Debugf("設定ファイル: %s", cfg.Path)
	}

	if o.List {
		return list(ctx, cfg, o, stderr, log)
	}
	dev, err := cfg.Resolve(o.Device)
	if err != nil {
		log.Errorf("%v", err)
		return 2
	}
	if o.Status {
		return status(ctx, cfg, dev, stderr, log)
	}

	plan, err := args.Build(o, cfg, dev)
	if err != nil {
		log.Errorf("%v", err)
		return 2
	}
	for _, w := range plan.Warnings {
		log.Warnf("%s", w)
	}
	for _, n := range plan.Notes {
		log.Infof("%s", n)
	}
	log.Infof("デバイス %s（%s 接続）、チャンネル %s、%s モード", dev.Name, dev.Connection, plan.Channel.Name, plan.Mode)

	out, closeOut, err := openOutput(o.Output, stdout)
	if err != nil {
		log.Errorf("%v", err)
		return 1
	}
	res, err := runner.Run(ctx, runner.Config{
		Stages:           plan.Stages,
		Out:              out,
		Stderr:           stderr,
		Limit:            plan.Limit,
		FirstByteTimeout: seconds(cfg.FirstByteTimeout),
		StopGrace:        seconds(cfg.StopGrace),
		Log:              log,
	})
	if cerr := closeOut(); cerr != nil && err == nil {
		err = fmt.Errorf("出力ファイルを閉じられません: %w", cerr)
	}
	log.Infof("終了: %s（%d バイト）", res.Reason, res.Bytes)
	switch {
	case errors.Is(err, runner.ErrOutputClosed):
		// 録画サーバーがパイプを閉じた。正常な停止として扱う。
		return 0
	case err != nil:
		log.Errorf("%v", err)
		return 1
	}
	if res.Reason == "eof" && (o.Unlimited || dev.Connection == config.ConnServe) {
		log.Warnf("dth が録画時間の前に終了しました")
	}
	return 0
}

func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// openOutput は出力先を開く。"-" は標準出力。
func openOutput(path string, stdout io.Writer) (io.Writer, func() error, error) {
	if path == "-" {
		return stdout, func() error { return nil }, nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("出力ファイルを開けません: %w", err)
	}
	return f, f.Close, nil
}

// list は dth list の結果と設定済みデバイスを stderr に表示する（stdout は TS 専用なので使わない）。
func list(ctx context.Context, cfg *config.Config, o *args.Options, stderr io.Writer, log *logx.Logger) int {
	bin := cfg.DthBin
	if o.Device != "" {
		if d, err := cfg.Resolve(o.Device); err == nil {
			bin = cfg.DthBinFor(d)
		}
	}
	fmt.Fprintln(stderr, "== dth list ==")
	code := runTool(ctx, stderr, log, bin, "list")
	fmt.Fprintln(stderr, "== 設定済みデバイス ==")
	if len(cfg.Devices) == 0 {
		fmt.Fprintln(stderr, "（なし）")
	}
	for _, n := range cfg.Names() {
		d := cfg.Devices[n]
		mark := ""
		if n == cfg.DefaultDevice {
			mark = " *既定"
		}
		where := d.Serial
		if d.Connection == config.ConnServe {
			where = d.Socket + " " + d.Serial
		}
		fmt.Fprintf(stderr, "%-10s %-10s %-6s %-13s %s%s\n", n, d.Model, d.Connection, d.ModePolicy, strings.TrimSpace(where), mark)
	}
	return code
}

// status は dth status を stderr に表示する。
func status(ctx context.Context, cfg *config.Config, d *config.Device, stderr io.Writer, log *logx.Logger) int {
	if d.Connection != config.ConnServe {
		log.Errorf("デバイス %s は direct 接続です（--status は dth serve 用。--list を使ってください）", d.Name)
		return 2
	}
	a := []string{"status", "--socket", d.Socket}
	if d.Serial != "" {
		a = append(a, "--serial", d.Serial)
	}
	return runTool(ctx, stderr, log, cfg.DthBinFor(d), a...)
}

func runTool(ctx context.Context, stderr io.Writer, log *logx.Logger, bin string, a ...string) int {
	cmd := exec.CommandContext(ctx, bin, a...)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	log.Debugf("実行: %s %s", bin, strings.Join(a, " "))
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		log.Errorf("%s を実行できません: %v", bin, err)
		return 1
	}
	return 0
}
