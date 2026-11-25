package args

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/waya0125/recdth/internal/channel"
	"github.com/waya0125/recdth/internal/config"
)

const testConfig = `
dth_bin = "/usr/local/bin/dth"
b25_cmd = ["arib-b25-stream-test"]
default_device = "h70"

[devices.h70]
family = "dth10"
satellite = true
connection = "serve"
mode_policy = "device_only"
socket = "/run/dth/dth.sock"
serial = "H70SERIAL"

[devices.h70direct]
family = "dth10"
satellite = true
connection = "direct"
mode_policy = "device_only"
serial = "H70SERIAL"
app_keys = "/var/lib/dth/CoBDACom.dll"
lnb = 15

[devices.h45]
family = "dth30"
connection = "direct"
mode_policy = "raw_preferred"
serial = "DTH30-0128-3-1"
firmware = "/var/lib/dth/BufFsUSB.sys"

[devices.h45dev]
family = "dth30"
connection = "direct"
mode_policy = "device_only"
serial = "DTH30-0128-3-1"

[devices.h45auto]
family = "dth30"
connection = "serve"
socket = "/run/dth/dth.sock"
serial = "DTH30-0128-3-1"
takeover = true
`

func mustConfig(t *testing.T) *config.Config {
	t.Helper()
	c, err := config.Parse(testConfig)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func plan(t *testing.T, argv ...string) (*Plan, error) {
	t.Helper()
	o, err := ParseCLI(argv, io.Discard)
	if err != nil {
		t.Fatalf("ParseCLI(%q): %v", argv, err)
	}
	c := mustConfig(t)
	d, err := c.Resolve(o.Device)
	if err != nil {
		t.Fatal(err)
	}
	return Build(o, c, d)
}

func TestParseCLI(t *testing.T) {
	o, err := ParseCLI([]string{"--device", "h45", "--b25", "--sid", "1024", "27", "60", "out.ts"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.Device != "h45" || !o.B25 || o.SID != "1024" || o.Channel != "27" || o.Duration != 60*time.Second || o.Unlimited || o.Output != "out.ts" {
		t.Errorf("o = %+v", o)
	}
	// オプションが位置引数の後ろにあってもよい
	o, err = ParseCLI([]string{"BS15_0", "-", "-", "--device", "h70", "-v", "--verbose", "--lnb", "15"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if o.Channel != "BS15_0" || !o.Unlimited || o.Output != "-" || o.Device != "h70" || o.Verbose != 2 || o.LNB == nil || *o.LNB != 150 {
		t.Errorf("o = %+v", o)
	}
	o, err = ParseCLI([]string{"27", "0", "-"}, io.Discard)
	if err != nil || !o.Unlimited {
		t.Errorf("0 秒は無制限: %+v %v", o, err)
	}
	o, err = ParseCLI([]string{"--list"}, io.Discard)
	if err != nil || !o.List {
		t.Errorf("--list: %+v %v", o, err)
	}
	bad := [][]string{
		{"27", "60"},
		{"27", "abc", "-"},
		{"27", "-5", "-"},
		{"--mode", "foo", "27", "60", "-"},
		{"--lnb", "12", "27", "60", "-"},
		{"--list", "27"},
		{"--list", "--status"},
		{"27", "60", "-", "extra"},
	}
	for _, a := range bad {
		if _, err := ParseCLI(a, io.Discard); err == nil {
			t.Errorf("ParseCLI(%q) はエラーになるべき", a)
		}
	}
}

func TestParseLNB(t *testing.T) {
	for in, want := range map[string]int{"0": 0, "11": 110, "15": 150, "110": 110, "150": 150} {
		if got, err := ParseLNB(in); err != nil || got != want {
			t.Errorf("ParseLNB(%q) = %d, %v", in, got, err)
		}
	}
}

func TestBuildServeDevice(t *testing.T) {
	p, err := plan(t, "--device", "h70", "--sid", "101", "BS15_0", "60", "out.ts")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/local/bin/dth", "stream", "--socket", "/run/dth/dth.sock", "-c", "BS15_0", "--mode", "device", "--program", "101", "--serial", "H70SERIAL", "--if-free"}
	if !reflect.DeepEqual(p.Stages, [][]string{want}) {
		t.Errorf("stages = %q\nwant     %q", p.Stages, want)
	}
	if p.Limit != 60*time.Second {
		t.Errorf("serve は recdth が時間を数える: %v", p.Limit)
	}
}

func TestBuildCSAndDefaultDevice(t *testing.T) {
	p, err := plan(t, "CS4", "-", "-")
	if err != nil {
		t.Fatal(err)
	}
	argv := p.Stages[0]
	if p.Device.Name != "h70" || argv[5] != "ND04" || p.Program != "first" || p.Limit != 0 {
		t.Errorf("plan = %+v argv = %q", p, argv)
	}
}

func TestBuildDirectDevice(t *testing.T) {
	p, err := plan(t, "--device", "h70direct", "-v", "-v", "BS1_0", "30", "-")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/local/bin/dth", "-v", "tune", "-c", "BS01_0", "--mode", "device", "--program", "first", "-t", "30", "--serial", "H70SERIAL", "--app-keys", "/var/lib/dth/CoBDACom.dll", "--lnb", "150"}
	if !reflect.DeepEqual(p.Stages[0], want) {
		t.Errorf("argv = %q\nwant   %q", p.Stages[0], want)
	}
	if p.Limit != 30*time.Second+directWatchdog {
		t.Errorf("limit = %v", p.Limit)
	}
	// --lnb 0 で設定を打ち消す。地デジには --lnb を付けない
	p, err = plan(t, "--device", "h70direct", "--lnb", "0", "BS1_0", "30", "-")
	if err != nil || contains(p.Stages[0], "--lnb") {
		t.Errorf("--lnb 0: %q %v", p.Stages[0], err)
	}
	p, err = plan(t, "--device", "h70direct", "27", "30", "-")
	if err != nil || contains(p.Stages[0], "--lnb") {
		t.Errorf("地デジに --lnb: %q %v", p.Stages[0], err)
	}
}

func TestBuildRawB25(t *testing.T) {
	p, err := plan(t, "--device", "h45", "--b25", "27", "10", "x.ts")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"/usr/local/bin/dth", "tune", "-c", "27", "--mode", "raw", "-t", "10", "--serial", "DTH30-0128-3-1", "--firmware", "/var/lib/dth/BufFsUSB.sys"},
		{"arib-b25-stream-test"},
	}
	if !reflect.DeepEqual(p.Stages, want) {
		t.Errorf("stages = %q\nwant     %q", p.Stages, want)
	}
	// raw だが --b25 なし: B25 を通さない
	p, err = plan(t, "--device", "h45", "27", "10", "x.ts")
	if err != nil || len(p.Stages) != 1 || p.Mode != ModeRaw {
		t.Errorf("raw のみ: %+v %v", p, err)
	}
}

func TestB25PassthroughInDeviceMode(t *testing.T) {
	p, err := plan(t, "--device", "h45dev", "--b25", "27", "10", "x.ts")
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != ModeDevice || len(p.Stages) != 1 {
		t.Errorf("device モードでは外部 B25 を通さない: %+v", p)
	}
	if !strings.Contains(strings.Join(p.Notes, "\n"), "復号済み") {
		t.Errorf("notes = %q", p.Notes)
	}
	// dth30 には --app-keys を付けない
	if contains(p.Stages[0], "--app-keys") {
		t.Errorf("argv = %q", p.Stages[0])
	}
}

func TestDeviceOnlyRefusesRaw(t *testing.T) {
	for _, dev := range []string{"h70", "h70direct", "h45dev"} {
		_, err := plan(t, "--device", dev, "--mode", "raw", "27", "10", "-")
		if !errors.Is(err, ErrRawRefused) {
			t.Errorf("%s で --mode raw: %v", dev, err)
		}
	}
	// device_only では --b25 / --sid all でも auto が raw を選ばない
	for _, a := range [][]string{{"--b25"}, {"--sid", "all"}} {
		p, err := plan(t, append(append([]string{"--device", "h70"}, a...), "27", "10", "-")...)
		if err != nil || p.Mode != ModeDevice {
			t.Errorf("device_only auto %q: %+v %v", a, p, err)
		}
	}
}

func TestSatelliteNeverRaw(t *testing.T) {
	_, err := plan(t, "--device", "h45auto", "--mode", "raw", "BS15_0", "10", "-")
	if err == nil {
		t.Error("BS で --mode raw はエラーになるべき")
	}
	// dth30 は地デジ専用
	if _, err := plan(t, "--device", "h45", "BS15_0", "10", "-"); err == nil {
		t.Error("dth30 で BS はエラーになるべき")
	}
}

func TestSatelliteDeviceRawNeedsAllowRaw(t *testing.T) {
	c, err := config.Parse("[devices.x]\nsatellite = true\n")
	if err != nil {
		t.Fatal(err)
	}
	o := &Options{Mode: "raw", Channel: "27", Unlimited: true, Output: "-"}
	if _, err := Build(o, c, c.Devices["x"]); !errors.Is(err, ErrRawRefused) {
		t.Errorf("allow_raw なしの BS/CS 対応機で raw: %v", err)
	}
	o.Mode = "auto"
	o.B25 = true
	c.B25Cmd = []string{"b25"}
	if p, err := Build(o, c, c.Devices["x"]); err != nil || p.Mode != ModeDevice {
		t.Errorf("auto は raw を選ばない: %+v %v", p, err)
	}
	c.Devices["x"].AllowRaw = true
	if p, err := Build(o, c, c.Devices["x"]); err != nil || p.Mode != ModeRaw {
		t.Errorf("allow_raw = true なら auto + --b25 で raw: %+v %v", p, err)
	}
}

func TestAutoPolicy(t *testing.T) {
	// h45auto は mode_policy 既定（auto）・serve・takeover
	p, err := plan(t, "--device", "h45auto", "27", "10", "-")
	if err != nil || p.Mode != ModeDevice || contains(p.Stages[0], "--if-free") {
		t.Errorf("auto 既定は device、takeover なら --if-free なし: %+v %v", p, err)
	}
	p, err = plan(t, "--device", "h45auto", "--b25", "27", "10", "-")
	if err != nil || p.Mode != ModeRaw || len(p.Stages) != 2 {
		t.Errorf("auto + --b25 は raw + B25: %+v %v", p, err)
	}
	p, err = plan(t, "--device", "h45auto", "--sid", "all", "27", "10", "-")
	if err != nil || p.Mode != ModeRaw || len(p.Stages) != 1 || !reflect.DeepEqual(p.RawSIDs, []string{"all"}) {
		t.Errorf("auto + --sid all は raw: %+v %v", p, err)
	}
	// 機種の分からない臨時デバイスでは auto で raw を選ばない
	p, err = plan(t, "--device", "/run/other.sock", "--b25", "27", "10", "-")
	if err != nil || p.Mode != ModeDevice {
		t.Errorf("臨時デバイス: %+v %v", p, err)
	}
	// 明示すれば使えるが警告を出す
	p, err = plan(t, "--device", "/run/other.sock", "--mode", "raw", "27", "10", "-")
	if err != nil || p.Mode != ModeRaw || len(p.Warnings) == 0 {
		t.Errorf("臨時デバイスで raw 明示: %+v %v", p, err)
	}
}

func TestResolveSID(t *testing.T) {
	cases := []struct {
		mode    Mode
		sid     string
		sidAll  string
		program string
		raw     []string
		warn    bool
		err     bool
	}{
		{ModeDevice, "", "fallback", "first", nil, false, false},
		{ModeDevice, "hd", "fallback", "first", nil, false, false},
		{ModeDevice, "HD", "fallback", "first", nil, false, false},
		{ModeDevice, "1024", "fallback", "1024", nil, false, false},
		{ModeDevice, "0x400", "fallback", "1024", nil, false, false},
		{ModeDevice, "1024,1025", "fallback", "1024", nil, true, false},
		{ModeDevice, "all", "fallback", "first", nil, true, false},
		{ModeDevice, "all", "error", "", nil, false, true},
		{ModeDevice, "abc", "fallback", "", nil, false, true},
		{ModeDevice, "0", "fallback", "", nil, false, true},
		{ModeDevice, "70000", "fallback", "", nil, false, true},
		{ModeRaw, "", "fallback", "", nil, false, false},
		{ModeRaw, "all", "fallback", "", []string{"all"}, false, false},
		{ModeRaw, "1024,0x401", "fallback", "", []string{"1024", "1025"}, false, false},
		{ModeRaw, "x", "fallback", "", nil, false, true},
	}
	for _, c := range cases {
		prog, raw, warn, err := ResolveSID(c.mode, c.sid, c.sidAll)
		if (err != nil) != c.err || prog != c.program || !reflect.DeepEqual(raw, c.raw) || (len(warn) > 0) != c.warn {
			t.Errorf("ResolveSID(%s, %q, %s) = %q %q %q %v", c.mode, c.sid, c.sidAll, prog, raw, warn, err)
		}
	}
}

func TestB25WithoutCommand(t *testing.T) {
	c, err := config.Parse("[devices.x]\nfamily = \"dth30\"\nmode_policy = \"raw_preferred\"\n")
	if err != nil {
		t.Fatal(err)
	}
	o := &Options{Mode: "auto", B25: true, Channel: "27", Unlimited: true, Output: "-"}
	if _, err := Build(o, c, c.Devices["x"]); err == nil || !strings.Contains(err.Error(), "b25_cmd") {
		t.Errorf("b25_cmd 未設定: %v", err)
	}
}

func TestServeIgnoresLNB(t *testing.T) {
	p, err := plan(t, "--device", "h70", "--lnb", "15", "BS15_0", "10", "-")
	if err != nil || contains(p.Stages[0], "--lnb") || len(p.Warnings) == 0 {
		t.Errorf("serve で --lnb: %+v %v", p, err)
	}
}

func TestChannelTypeForMirakurun(t *testing.T) {
	// Mirakurun の channels.yml でよく使う表記がそのまま通ること
	for in, want := range map[string]string{"27": "27", "BS15_0": "BS15_0", "BS01_0": "BS01_0", "CS2": "ND02", "CS24": "ND24"} {
		c, err := channel.Parse(in)
		if err != nil || c.Name != want {
			t.Errorf("%s → %+v %v", in, c, err)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestChannelWithSID(t *testing.T) {
	p, err := plan(t, "--device", "h70", "BS15_0:101", "-", "-")
	if err != nil || p.Program != "101" || p.Channel.Name != "BS15_0" || !contains(p.Stages[0], "101") {
		t.Errorf("BS15_0:101: %+v %v", p, err)
	}
	p, err = plan(t, "--device", "h70", "--sid", "1024", "27:1024", "-", "-")
	if err != nil || p.Program != "1024" {
		t.Errorf("同じ値の --sid は可: %+v %v", p, err)
	}
	p, err = plan(t, "--device", "h45", "27:1024", "-", "-")
	if err != nil || p.Mode != ModeRaw || !reflect.DeepEqual(p.RawSIDs, []string{"1024"}) {
		t.Errorf("raw では TS フィルター用に保持: %+v %v", p, err)
	}
	if _, err := plan(t, "--device", "h70", "--sid", "1025", "27:1024", "-", "-"); err == nil {
		t.Error("--sid と食い違う SID はエラー")
	}
	if _, err := plan(t, "--device", "h70", "27:", "-", "-"); err == nil {
		t.Error("空の SID はエラー")
	}
}
