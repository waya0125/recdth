package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExampleConfig(t *testing.T) {
	c, err := Load(filepath.Join("..", "..", "config.example.toml"))
	if err != nil {
		t.Fatal(err)
	}
	h70 := c.Devices["h70"]
	if h70 == nil || h70.Connection != ConnServe || h70.ModePolicy != PolicyDeviceOnly || !h70.Satellite || h70.Socket != "/run/dth/dth.sock" {
		t.Errorf("h70 = %+v", h70)
	}
	h45 := c.Devices["h45"]
	if h45 == nil || h45.Connection != ConnDirect || h45.ModePolicy != PolicyRawPreferred || h45.Family != FamilyDTH30 {
		t.Errorf("h45 = %+v", h45)
	}
	if c.DefaultDevice != "h70" || c.SidAll != SidAllFallback || len(c.B25Cmd) != 1 {
		t.Errorf("config = %+v", c)
	}
}

func TestDefaults(t *testing.T) {
	c, err := Parse(`[devices.a]
socket = "/run/dth/a.sock"
[devices.b]
`)
	if err != nil {
		t.Fatal(err)
	}
	if c.DthBin != "dth" || c.StopGrace != DefaultStopGrace || c.SidAll != SidAllFallback || c.FirstByteTimeout != DefaultFirstByteTimeout {
		t.Errorf("globals = %+v", c)
	}
	if a := c.Devices["a"]; a.Connection != ConnServe || a.ModePolicy != PolicyAuto || a.Name != "a" {
		t.Errorf("a = %+v", a)
	}
	if b := c.Devices["b"]; b.Connection != ConnDirect {
		t.Errorf("b = %+v", b)
	}
}

func TestInvalid(t *testing.T) {
	cases := map[string]string{
		"未知のキー":                 "dth_binn = \"x\"",
		"未知のデバイスキー":             "[devices.a]\nsokcet = \"/x\"",
		"serve に socket なし":     "[devices.a]\nconnection = \"serve\"",
		"不正な接続方式":               "[devices.a]\nconnection = \"usb\"",
		"不正なモード方針":              "[devices.a]\nmode_policy = \"raw\"",
		"BS/CS機の raw_preferred": "[devices.a]\nsatellite = true\nmode_policy = \"raw_preferred\"",
		"dth30 の satellite":     "[devices.a]\nfamily = \"dth30\"\nsatellite = true",
		"不正な family":            "[devices.a]\nfamily = \"dth20\"",
		"不正な lnb":               "[devices.a]\nlnb = 12",
		"direct の tuner auto":   "[devices.a]\ntuner = \"auto\"",
		"不正な sid_all":           "sid_all = \"ignore\"",
		"存在しない default":         "default_device = \"zz\"\n[devices.a]",
	}
	for name, s := range cases {
		if _, err := Parse(s); err == nil {
			t.Errorf("%s: エラーになるべき\n%s", name, s)
		}
	}
	// 承知の上なら raw_preferred を許可
	if _, err := Parse("[devices.a]\nsatellite = true\nallow_raw = true\nmode_policy = \"raw_preferred\""); err != nil {
		t.Errorf("allow_raw: %v", err)
	}
}

func TestNormalizeLNB(t *testing.T) {
	for in, want := range map[int]int{0: 0, 11: 110, 15: 150, 110: 110, 150: 150} {
		got, err := NormalizeLNB(in)
		if err != nil || got != want {
			t.Errorf("NormalizeLNB(%d) = %d, %v", in, got, err)
		}
	}
	if _, err := NormalizeLNB(5); err == nil {
		t.Error("NormalizeLNB(5) はエラーになるべき")
	}
}

func TestResolve(t *testing.T) {
	c, err := Parse(`
[devices.h70]
satellite = true
socket = "/run/dth/dth.sock"
serial = "ABC123"
[devices.h45]
family = "dth30"
serial = "DTH30-0128-3-1"
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Resolve(""); err == nil || !strings.Contains(err.Error(), "--device") {
		t.Errorf("複数台で --device 省略はエラー: %v", err)
	}
	for sel, want := range map[string]string{"h70": "h70", "h45": "h45", "/run/dth/dth.sock": "h70", "ABC123": "h70", "DTH30-0128-3-1": "h45"} {
		d, err := c.Resolve(sel)
		if err != nil || d.Name != want || d.Adhoc {
			t.Errorf("Resolve(%q) = %+v, %v; want %s", sel, d, err, want)
		}
	}
	d, err := c.Resolve("/run/other.sock")
	if err != nil || !d.Adhoc || d.Connection != ConnServe || d.Socket != "/run/other.sock" {
		t.Errorf("臨時ソケット = %+v, %v", d, err)
	}
	d, err = c.Resolve("SERIAL9")
	if err != nil || !d.Adhoc || d.Connection != ConnDirect || d.Serial != "SERIAL9" {
		t.Errorf("臨時シリアル = %+v, %v", d, err)
	}

	c.DefaultDevice = "h45"
	if d, _ := c.Resolve(""); d.Name != "h45" {
		t.Errorf("default_device が使われない: %+v", d)
	}
	empty, _ := Parse("")
	if d, err := empty.Resolve(""); err != nil || d.Connection != ConnDirect || !d.Adhoc {
		t.Errorf("定義なし = %+v, %v", d, err)
	}
}

func TestFind(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p := filepath.Join(dir, "recdth", "config.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("dth_bin = \"/opt/dth\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECDTH_CONFIG", "")
	c, err := Find("")
	if err != nil || c.DthBin != "/opt/dth" || c.Path != p {
		t.Errorf("Find = %+v, %v", c, err)
	}
	if _, err := Find(filepath.Join(dir, "missing.toml")); err == nil {
		t.Error("明示した設定ファイルが無ければエラー")
	}
}
