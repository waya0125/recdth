// Package args は recdth のコマンドライン解釈と、dth・外部 B25 の引数の組み立てを行う。
package args

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Usage は使い方の先頭部分。
const Usage = `使い方: recdth [オプション] <チャンネル> <秒数|-> <出力先|->
       recdth --list | --status [--device 名前]

  <チャンネル>  地デジ 13〜62 / BS15_0, BS1_0 / CS2〜CS24, ND02〜ND24（"BS15_0:101" のように :SID も可）
  <秒数>        録画秒数。- または 0 で SIGINT / SIGTERM まで継続
  <出力先>      出力ファイル。- で標準出力（標準出力には TS 以外を書きません）

オプション:
`

// Options はコマンドラインの内容。
type Options struct {
	Device  string
	Mode    string // auto / device / raw
	SID     string
	TSID    uint16 // 明示された TSID（0 = 未指定）
	B25     bool
	LNB     *int // 指定されたときだけ（dth の表記 0/110/150 に変換済み）
	Config  string
	List    bool
	Status  bool
	Verbose int

	Channel   string
	Duration  time.Duration // Unlimited のときは 0
	Unlimited bool
	Output    string
}

// countFlag は -v を重ねるたびに増える。
type countFlag struct{ n *int }

func (c countFlag) String() string {
	if c.n == nil {
		return "0"
	}
	return strconv.Itoa(*c.n)
}
func (c countFlag) Set(string) error { *c.n++; return nil }
func (c countFlag) IsBoolFlag() bool { return true }

// tsidFlag は --tsid <HEX>（例: 4971 / 0x4971）を受ける。
type tsidFlag struct{ v *uint16 }

func (t tsidFlag) String() string {
	if t.v == nil || *t.v == 0 {
		return ""
	}
	return fmt.Sprintf("%04x", *t.v)
}
func (t tsidFlag) Set(s string) error {
	v, err := ParseTSID(s)
	if err != nil {
		return err
	}
	*t.v = v
	return nil
}

// ParseTSID は 16 進の TSID（例: "4971" / "0x4971"、1〜0xffff）を解釈する。
func ParseTSID(s string) (uint16, error) {
	in := strings.TrimSpace(s)
	digits := strings.TrimPrefix(strings.TrimPrefix(in, "0x"), "0X")
	if digits == "" {
		return 0, fmt.Errorf("--tsid の値 %q を解釈できません（例: 4971 / 0x4971）", s)
	}
	v, err := strconv.ParseUint(digits, 16, 16)
	if err != nil || v == 0 {
		return 0, fmt.Errorf("--tsid の値 %q を解釈できません（例: 4971 / 0x4971）", s)
	}
	return uint16(v), nil
}

// lnbFlag は --lnb 0|11|15 を受ける（dth 表記の 110/150 も許可）。
type lnbFlag struct{ v **int }

func (l lnbFlag) String() string {
	if l.v == nil || *l.v == nil {
		return ""
	}
	return strconv.Itoa(**l.v)
}
func (l lnbFlag) Set(s string) error {
	n, err := ParseLNB(s)
	if err != nil {
		return err
	}
	*l.v = &n
	return nil
}

// ParseLNB は 0 / 11 / 15（110 / 150 も可）を dth の表記 0 / 110 / 150 にする。
func ParseLNB(s string) (int, error) {
	switch strings.TrimSpace(s) {
	case "0":
		return 0, nil
	case "11", "110":
		return 110, nil
	case "15", "150":
		return 150, nil
	}
	return 0, fmt.Errorf("--lnb %q は 0 / 11 / 15 のいずれかです", s)
}

// ParseCLI は argv（プログラム名を除く）を解釈する。オプションは位置引数の前後どちらにも書ける。
func ParseCLI(argv []string, stderr io.Writer) (*Options, error) {
	o := &Options{Mode: "auto"}
	fs := flag.NewFlagSet("recdth", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, Usage)
		fs.PrintDefaults()
	}
	fs.StringVar(&o.Device, "device", "", "設定ファイルの論理デバイス名（h70 など）、dth serve のソケットパス、またはシリアル／ユニット名")
	fs.StringVar(&o.Mode, "mode", "auto", "auto / device / raw（auto は設定のモード方針とオプションから決める）")
	fs.StringVar(&o.SID, "sid", "", "device モード: SID（--program に変換）/ hd（= first）/ all（1 番組制限のため first に戻すかエラー）。raw モード: 将来の TS フィルター用")
	fs.Var(tsidFlag{&o.TSID}, "tsid", "BS/CS の TSID を 16 進で明示（例: 4971 / 0x4971）。BS は省略時にも既知の表から自動付与する")
	fs.BoolVar(&o.B25, "b25", false, "raw モード: 外部 B25 復号コマンドに通す。device モード: 内蔵 B-CAS で復号済みなので何もしない")
	fs.Var(lnbFlag{&o.LNB}, "lnb", "BS/CS のアンテナ電源 0 / 11 / 15（dth の 0 / 110 / 150）。direct 接続のときだけ効く")
	fs.StringVar(&o.Config, "config", "", "設定ファイル（既定: ~/.config/recdth/config.toml → /etc/recdth/config.toml）")
	fs.BoolVar(&o.List, "list", false, "デバイス一覧（dth list）と設定済みデバイスを表示する")
	fs.BoolVar(&o.Status, "status", false, "dth serve の状態（dth status）を表示する")
	fs.Var(countFlag{&o.Verbose}, "v", "詳しいログ（-v で情報、-v -v で dth にも -v を渡す）")
	fs.Var(countFlag{&o.Verbose}, "verbose", "-v と同じ")

	pos, err := parseInterspersed(fs, argv)
	if err != nil {
		return nil, err
	}
	switch o.Mode {
	case "auto", "device", "raw":
	default:
		return nil, fmt.Errorf("--mode %q は auto / device / raw のいずれかです", o.Mode)
	}
	if o.List || o.Status {
		if o.List && o.Status {
			return nil, errors.New("--list と --status は同時に指定できません")
		}
		if len(pos) > 0 {
			return nil, errors.New("--list / --status にチャンネル等は指定できません")
		}
		return o, nil
	}
	if len(pos) != 3 {
		fs.Usage()
		return nil, errors.New("チャンネル・秒数・出力先の 3 つを指定してください")
	}
	o.Channel = pos[0]
	if o.Duration, o.Unlimited, err = ParseDuration(pos[1]); err != nil {
		return nil, err
	}
	o.Output = pos[2]
	if o.Output == "" {
		return nil, errors.New("出力先が空です")
	}
	return o, nil
}

// ParseDuration は秒数を解釈する。"-" と "0" は無制限。
func ParseDuration(s string) (time.Duration, bool, error) {
	if s == "-" {
		return 0, true, nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n < 0 || n != n || n > 1e7 {
		return 0, false, fmt.Errorf("秒数 %q を解釈できません（- または 0 で無制限）", s)
	}
	if n == 0 {
		return 0, true, nil
	}
	return time.Duration(n * float64(time.Second)), false, nil
}

// parseInterspersed はオプションと位置引数が混ざった argv を解釈する。"--" 以降はすべて位置引数。
func parseInterspersed(fs *flag.FlagSet, argv []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(argv); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		consumed := len(argv) - len(rest)
		if consumed > 0 && argv[consumed-1] == "--" {
			return append(pos, rest...), nil
		}
		pos = append(pos, rest[0])
		argv = rest[1:]
	}
}
