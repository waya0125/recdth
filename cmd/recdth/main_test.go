//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEnv は偽の dth と偽の B25 コマンド、設定ファイルを用意する。
// 偽の dth は受け取った引数を $ARGS に書き、TS のかわりに "TS" を 3 回書いて終わる。
func fakeEnv(t *testing.T) (cfgPath, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	dth := filepath.Join(dir, "dth")
	write(t, dth, "#!/bin/sh\necho \"$@\" >> \""+argsFile+"\"\nprintf TSTSTS\n", 0o755)
	b25 := filepath.Join(dir, "b25")
	write(t, b25, "#!/bin/sh\necho b25 >> \""+argsFile+"\"\ntr T D\n", 0o755)
	cfgPath = filepath.Join(dir, "config.toml")
	write(t, cfgPath, `dth_bin = "`+dth+`"
b25_cmd = ["`+b25+`"]
default_device = "h70"
[devices.h70]
family = "dth10"
satellite = true
connection = "serve"
mode_policy = "device_only"
socket = "/run/dth/dth.sock"
[devices.h45]
family = "dth30"
connection = "direct"
mode_policy = "raw_preferred"
serial = "DTH30-0128-3-1"
`, 0o644)
	return cfgPath, argsFile
}

func write(t *testing.T, p, s string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), mode); err != nil {
		t.Fatal(err)
	}
}

func TestRunServeToStdout(t *testing.T) {
	cfg, argsFile := fakeEnv(t)
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--config", cfg, "CS2", "-", "-"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.String() != "TSTSTS" {
		t.Errorf("stdout には TS だけ: %q", stdout.String())
	}
	got, _ := os.ReadFile(argsFile)
	want := "stream --socket /run/dth/dth.sock -c ND02 --mode device --program first --if-free\n"
	if string(got) != want {
		t.Errorf("dth の引数 = %q\nwant %q", got, want)
	}
	if !strings.Contains(stderr.String(), "録画時間の前に終了") {
		t.Errorf("早期終了の警告が出ていない: %s", stderr.String())
	}
}

func TestRunRawB25ToFile(t *testing.T) {
	cfg, argsFile := fakeEnv(t)
	out := filepath.Join(t.TempDir(), "out.ts")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--config", cfg, "--device", "h45", "--b25", "-v", "27", "10", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("ファイル出力のとき stdout は空: %q", stdout.String())
	}
	b, _ := os.ReadFile(out)
	if string(b) != "DSDSDS" {
		t.Errorf("B25 を通した出力 = %q", b)
	}
	got, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(got), "tune -c 27 --mode raw -t 10 --serial DTH30-0128-3-1") || !strings.Contains(string(got), "b25") {
		t.Errorf("引数 = %q", got)
	}
}

func TestRunRefusesRawOnDeviceOnly(t *testing.T) {
	cfg, argsFile := fakeEnv(t)
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--config", cfg, "--mode", "raw", "27", "10", "-"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "device_only") {
		t.Errorf("code = %d, stderr = %s", code, stderr.String())
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Error("拒否したのに dth が起動された")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestListWritesOnlyToStderr(t *testing.T) {
	cfg, _ := fakeEnv(t)
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--config", cfg, "--list"}, &stdout, &stderr)
	if code != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "h45") {
		t.Errorf("code = %d stdout = %q stderr = %s", code, stdout.String(), stderr.String())
	}
}
