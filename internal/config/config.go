// Package config は recdth の TOML 設定ファイルを読む。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// 接続方式。
const (
	ConnServe  = "serve"  // dth stream --socket ...（dth serve 常駐）
	ConnDirect = "direct" // dth tune ...（その都度デバイスを開く）
)

// モード方針。
const (
	PolicyDeviceOnly   = "device_only"   // 常に device モード。raw は拒否する（安全弁）
	PolicyRawPreferred = "raw_preferred" // 地デジは raw（外部 B25 復号前提）、BS/CS は device
	PolicyAuto         = "auto"          // 既定 device。--b25 か --sid all のときだけ地デジを raw にする
)

// --sid all を device モードで受けたときの扱い。
const (
	SidAllFallback = "fallback" // 警告して first にする
	SidAllError    = "error"    // エラーにする
)

// 機種ファミリー。
const (
	FamilyDTH10 = "dth10"
	FamilyDTH30 = "dth30"
)

// Config は設定ファイル全体。
type Config struct {
	// DthBin は dth 実行ファイル（既定 "dth"、PATH から探す）。
	DthBin string `toml:"dth_bin"`
	// B25Cmd は raw モード + --b25 で通す外部 B25 復号コマンド（標準入力→標準出力）。
	B25Cmd []string `toml:"b25_cmd"`
	// DefaultDevice は --device 省略時の論理デバイス名。
	DefaultDevice string `toml:"default_device"`
	// SidAll は device モードで --sid all を受けたときの扱い（fallback / error）。
	SidAll string `toml:"sid_all"`
	// StopGrace は子プロセスに SIGTERM を送ってから SIGKILL するまでの秒数。
	StopGrace float64 `toml:"stop_grace"`
	// FirstByteTimeout は起動から最初の TS が届くまでの上限秒数（0 で無効）。
	FirstByteTimeout float64 `toml:"first_byte_timeout"`
	// Devices は論理デバイス名ごとのプロファイル。
	Devices map[string]*Device `toml:"devices"`

	// Path は読み込んだファイル（組み込みの既定値なら空）。
	Path string `toml:"-"`
}

// Device は 1 台分のプロファイル。
type Device struct {
	// Model は表示用の型番（例: "DT-H70/U2"）。
	Model string `toml:"model"`
	// Family は dth10 / dth30。dth30 は BS/CS 非対応、--app-keys 不要。
	Family string `toml:"family"`
	// Satellite は BS/CS を受信できる機種なら true（DT-H70/U2 など）。
	// raw を一度使うと電源再投入まで BS/CS が受信できなくなるため、raw を既定で拒否する。
	Satellite bool `toml:"satellite"`
	// AllowRaw は Satellite = true の機種で、それを承知で raw を許可するとき true。
	AllowRaw bool `toml:"allow_raw"`
	// Connection は serve / direct。
	Connection string `toml:"connection"`
	// ModePolicy は device_only / raw_preferred / auto。
	ModePolicy string `toml:"mode_policy"`
	// Socket は dth serve のソケット（serve のとき必須）。
	Socket string `toml:"socket"`
	// Serial は USB シリアル番号、DTH30 系はユニット名（dth list で確認）。
	Serial string `toml:"serial"`
	// AppKeys は DTH10 系 device モードで使う CoBDACom.dll（direct のとき dth tune に渡す）。
	AppKeys string `toml:"app_keys"`
	// Firmware は DTH30 系で純正ファームウェアを使うときの BufFsUSB.sys（direct のとき）。
	Firmware string `toml:"firmware"`
	// Lnb は BS/CS のアンテナ電源（0 / 11 / 15、または dth 表記の 0 / 110 / 150）。direct のときだけ効く。
	Lnb int `toml:"lnb"`
	// Tuner は 2 チューナー機の地デジ側チューナー（"0" / "1"、serve では "auto" も可）。
	Tuner string `toml:"tuner"`
	// Takeover は serve で他のクライアントが使用中のときに奪うなら true（既定は --if-free で失敗させる）。
	Takeover bool `toml:"takeover"`
	// DthBin / B25Cmd はグローバル設定の上書き。
	DthBin string   `toml:"dth_bin"`
	B25Cmd []string `toml:"b25_cmd"`

	// Name は論理デバイス名（設定のキー）。
	Name string `toml:"-"`
	// Adhoc は設定ファイルに無く --device の値から作ったプロファイルなら true。
	Adhoc bool `toml:"-"`
}

// 既定値。
const (
	DefaultDthBin           = "dth"
	DefaultStopGrace        = 5.0
	DefaultFirstByteTimeout = 30.0
	SystemPath              = "/etc/recdth/config.toml"
)

// DefaultPaths は --config 省略時に探すパスを優先順に返す。
// $RECDTH_CONFIG → $XDG_CONFIG_HOME/recdth/config.toml（無ければ ~/.config/recdth/config.toml）→ /etc/recdth/config.toml
func DefaultPaths() []string {
	var p []string
	if env := os.Getenv("RECDTH_CONFIG"); env != "" {
		p = append(p, env)
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		p = append(p, filepath.Join(x, "recdth", "config.toml"))
	} else if h, err := os.UserHomeDir(); err == nil {
		p = append(p, filepath.Join(h, ".config", "recdth", "config.toml"))
	}
	return append(p, SystemPath)
}

// Find は explicit が空でなければそれを読み（無ければエラー）、空なら DefaultPaths の最初に存在するものを読む。
// どれも無ければ組み込みの既定値（デバイス定義なし）を返す。
func Find(explicit string) (*Config, error) {
	if explicit != "" {
		return Load(explicit)
	}
	for _, p := range DefaultPaths() {
		if _, err := os.Stat(p); err == nil {
			return Load(p)
		}
	}
	c := &Config{}
	if err := c.normalize(); err != nil {
		return nil, err
	}
	return c, nil
}

// Load は path の設定ファイルを読む。
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("設定ファイルを読めません: %w", err)
	}
	c, err := Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Path = path
	return c, nil
}

// Parse は TOML の文字列を解釈する。未知のキーは書き間違いとしてエラーにする。
func Parse(s string) (*Config, error) {
	c := &Config{}
	md, err := toml.Decode(s, c)
	if err != nil {
		return nil, err
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, len(und))
		for i, k := range und {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("未知の設定項目: %s", strings.Join(keys, ", "))
	}
	if err := c.normalize(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) normalize() error {
	if c.DthBin == "" {
		c.DthBin = DefaultDthBin
	}
	if c.StopGrace == 0 {
		c.StopGrace = DefaultStopGrace
	}
	if c.StopGrace < 0 {
		return errors.New("stop_grace は 0 以上にしてください")
	}
	if c.FirstByteTimeout == 0 {
		c.FirstByteTimeout = DefaultFirstByteTimeout
	}
	switch c.SidAll {
	case "":
		c.SidAll = SidAllFallback
	case SidAllFallback, SidAllError:
	default:
		return fmt.Errorf("sid_all %q は fallback / error のどちらかです", c.SidAll)
	}
	if c.Devices == nil {
		c.Devices = map[string]*Device{}
	}
	for name, d := range c.Devices {
		if d == nil {
			return fmt.Errorf("devices.%s が空です", name)
		}
		d.Name = name
		if err := d.normalize(); err != nil {
			return fmt.Errorf("devices.%s: %w", name, err)
		}
	}
	if c.DefaultDevice != "" {
		if _, ok := c.Devices[c.DefaultDevice]; !ok {
			return fmt.Errorf("default_device %q が devices にありません", c.DefaultDevice)
		}
	}
	return nil
}

func (d *Device) normalize() error {
	switch d.Family {
	case "", FamilyDTH10:
	case FamilyDTH30:
		if d.Satellite {
			return errors.New("dth30 系は地デジ専用です（satellite = true は指定できません）")
		}
	default:
		return fmt.Errorf("family %q は dth10 / dth30 のどちらかです", d.Family)
	}
	if d.Connection == "" {
		if d.Socket != "" {
			d.Connection = ConnServe
		} else {
			d.Connection = ConnDirect
		}
	}
	switch d.Connection {
	case ConnServe:
		if d.Socket == "" {
			return errors.New("connection = \"serve\" には socket が必要です")
		}
	case ConnDirect:
	default:
		return fmt.Errorf("connection %q は serve / direct のどちらかです", d.Connection)
	}
	if d.ModePolicy == "" {
		d.ModePolicy = PolicyAuto
	}
	switch d.ModePolicy {
	case PolicyDeviceOnly, PolicyAuto:
	case PolicyRawPreferred:
		if d.Satellite && !d.AllowRaw {
			return errors.New("BS/CS 対応機で raw_preferred にすると、raw を使った時点で電源再投入まで BS/CS が受信できなくなります。承知の上なら allow_raw = true を指定してください")
		}
	default:
		return fmt.Errorf("mode_policy %q は device_only / raw_preferred / auto のいずれかです", d.ModePolicy)
	}
	lnb, err := NormalizeLNB(d.Lnb)
	if err != nil {
		return err
	}
	d.Lnb = lnb
	switch d.Tuner {
	case "", "0", "1":
	case "auto":
		if d.Connection != ConnServe {
			return errors.New("tuner = \"auto\" は connection = \"serve\" のときだけ使えます")
		}
	default:
		return fmt.Errorf("tuner %q は 0 / 1 / auto のいずれかです", d.Tuner)
	}
	return nil
}

// NormalizeLNB は 0 / 11 / 15（recpt1 系の表記）と 0 / 110 / 150（dth の表記）を dth の表記にそろえる。
func NormalizeLNB(v int) (int, error) {
	switch v {
	case 0:
		return 0, nil
	case 11, 110:
		return 110, nil
	case 15, 150:
		return 150, nil
	}
	return 0, fmt.Errorf("lnb %d は 0 / 11 / 15 のいずれかです", v)
}

// DthBinFor はデバイスの dth 実行ファイルを返す。
func (c *Config) DthBinFor(d *Device) string {
	if d != nil && d.DthBin != "" {
		return d.DthBin
	}
	return c.DthBin
}

// B25CmdFor はデバイスの外部 B25 コマンドを返す（未設定なら nil）。
func (c *Config) B25CmdFor(d *Device) []string {
	if d != nil && len(d.B25Cmd) > 0 {
		return d.B25Cmd
	}
	return c.B25Cmd
}

// Names は論理デバイス名を並べて返す。
func (c *Config) Names() []string {
	n := make([]string, 0, len(c.Devices))
	for k := range c.Devices {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

// Resolve は --device の値からプロファイルを選ぶ。
//
//   - 空: default_device、デバイスが 1 台だけならそれ、0 台なら組み込みの direct プロファイル
//   - 論理デバイス名: そのプロファイル
//   - "/" を含む値: ソケットパスとみなし、socket が一致するプロファイル、無ければ serve の臨時プロファイル
//   - それ以外: シリアル／ユニット名とみなし、serial が一致するプロファイル、無ければ direct の臨時プロファイル
//
// 臨時プロファイルは機種が分からないので Adhoc = true とし、auto で raw を選ばない。
func (c *Config) Resolve(sel string) (*Device, error) {
	if sel == "" {
		switch {
		case c.DefaultDevice != "":
			return c.Devices[c.DefaultDevice], nil
		case len(c.Devices) == 1:
			for _, d := range c.Devices {
				return d, nil
			}
		case len(c.Devices) == 0:
			return &Device{Name: "(既定)", Connection: ConnDirect, ModePolicy: PolicyAuto, Adhoc: true}, nil
		}
		return nil, fmt.Errorf("デバイスが複数定義されています。--device で選ぶか default_device を設定してください（%s）", strings.Join(c.Names(), ", "))
	}
	if d, ok := c.Devices[sel]; ok {
		return d, nil
	}
	if strings.Contains(sel, "/") {
		for _, n := range c.Names() {
			if d := c.Devices[n]; d.Connection == ConnServe && d.Socket == sel {
				return d, nil
			}
		}
		return &Device{Name: sel, Connection: ConnServe, Socket: sel, ModePolicy: PolicyAuto, Adhoc: true}, nil
	}
	for _, n := range c.Names() {
		if d := c.Devices[n]; d.Serial == sel {
			return d, nil
		}
	}
	return &Device{Name: sel, Connection: ConnDirect, Serial: sel, ModePolicy: PolicyAuto, Adhoc: true}, nil
}
