package args

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/waya0125/recdth/internal/channel"
	"github.com/waya0125/recdth/internal/config"
)

// Mode は dth の --mode に渡す値。
type Mode string

const (
	ModeDevice Mode = "device"
	ModeRaw    Mode = "raw"
)

// ErrRawRefused は安全弁で raw モードを拒否したときのエラー。
var ErrRawRefused = errors.New("raw モードは使えません")

// directWatchdog は direct 接続で dth tune に -t を渡したとき、recdth 側で待つ余裕。
// dth の -t は選局・ロック後から数えるため、その分（地デジ 1.2 秒・BS/CS 1.5 秒 + 最大 2 秒）より十分長くとる。
const directWatchdog = 20 * time.Second

// Plan は 1 回の録画で実行する内容。
type Plan struct {
	Device  *config.Device
	Channel channel.Channel
	Mode    Mode
	// Program は device モードで dth --program に渡す値（first または SID）。
	Program string
	// RawSIDs は raw モードで受けた --sid の値。将来の TS フィルター（PID 抽出）連携用のフックで、現状は使わない。
	RawSIDs []string
	// Stages はパイプでつなぐコマンド列。先頭が dth、続いて外部 B25（使う場合）。
	Stages [][]string
	// Limit は recdth が最初の TS を受けてから止めるまでの時間（0 = 無制限）。
	Limit time.Duration
	// Warnings は stderr に必ず出す警告、Notes は -v のときだけ出す説明。
	Warnings []string
	Notes    []string
}

// ResolveMode はチャンネル・デバイスのプロファイル・オプションから device / raw を決める。
//
//   - BS/CS は device だけ（dth に BS/CS の raw モードは無い）。--mode raw はエラー
//   - mode_policy = device_only のデバイスで --mode raw はエラー（安全弁）
//   - satellite = true（DT-H70/U2 など）で allow_raw が無ければ raw はエラー（raw 後は電源再投入まで BS/CS 不可）
//   - --mode auto: device_only → device、raw_preferred → raw、auto → --b25 か --sid all のときだけ raw
//     （機種の分からない臨時プロファイルでは auto で raw を選ばない）
func ResolveMode(o *Options, d *config.Device, ch channel.Channel) (Mode, []string, error) {
	var warn []string
	if ch.Satellite() {
		if o.Mode == string(ModeRaw) {
			return "", nil, fmt.Errorf("%w: BS/CS は device モードだけです（dth に BS/CS の raw モードはありません）", ErrRawRefused)
		}
		return ModeDevice, nil, nil
	}
	rawBlocked := func() error {
		if d.ModePolicy == config.PolicyDeviceOnly {
			return fmt.Errorf("%w: デバイス %s は mode_policy = \"device_only\" です", ErrRawRefused, d.Name)
		}
		if d.Satellite && !d.AllowRaw {
			return fmt.Errorf("%w: デバイス %s は BS/CS 対応機で、raw を一度使うと電源を入れ直すまで BS/CS が受信できなくなります。承知の上なら設定に allow_raw = true を書いてください", ErrRawRefused, d.Name)
		}
		return nil
	}
	switch o.Mode {
	case string(ModeDevice):
		return ModeDevice, nil, nil
	case string(ModeRaw):
		if err := rawBlocked(); err != nil {
			return "", nil, err
		}
		if d.Adhoc {
			warn = append(warn, "機種の分からないデバイスで raw を使います。DT-H70/U2 などの BS/CS 対応機では電源を入れ直すまで BS/CS が受信できなくなります")
		}
		return ModeRaw, warn, nil
	}
	// auto
	switch d.ModePolicy {
	case config.PolicyDeviceOnly:
		return ModeDevice, nil, nil
	case config.PolicyRawPreferred:
		if err := rawBlocked(); err != nil {
			return ModeDevice, []string{err.Error() + "（device モードで続けます）"}, nil
		}
		return ModeRaw, nil, nil
	default:
		wantsRaw := o.B25 || strings.EqualFold(strings.TrimSpace(o.SID), "all")
		if wantsRaw && !d.Adhoc && rawBlocked() == nil {
			return ModeRaw, nil, nil
		}
		return ModeDevice, nil, nil
	}
}

// ResolveSID は --sid を device モードの --program、または raw モードのフィルター用の値にする。
func ResolveSID(mode Mode, sid, sidAll string) (program string, raw []string, warn []string, err error) {
	s := strings.ToLower(strings.TrimSpace(sid))
	if mode == ModeRaw {
		if s == "" {
			return "", nil, nil, nil
		}
		for _, p := range strings.Split(s, ",") {
			p = strings.TrimSpace(p)
			if p == "hd" || p == "all" {
				raw = append(raw, p)
				continue
			}
			v, err := parseSID(p)
			if err != nil {
				return "", nil, nil, err
			}
			raw = append(raw, strconv.Itoa(v))
		}
		return "", raw, nil, nil
	}
	switch s {
	case "", "hd":
		return "first", nil, nil, nil
	case "all":
		if sidAll == config.SidAllError {
			return "", nil, nil, errors.New("device モードは 1 選局につき 1 番組だけ復号します。--sid all は使えません（全番組が必要なら raw モード + --b25）")
		}
		return "first", nil, []string{"device モードは 1 選局につき 1 番組だけ復号します。--sid all の代わりに最初の番組（first）を出力します"}, nil
	}
	parts := strings.Split(s, ",")
	v, err := parseSID(strings.TrimSpace(parts[0]))
	if err != nil {
		return "", nil, nil, err
	}
	if len(parts) > 1 {
		warn = append(warn, fmt.Sprintf("device モードは 1 選局につき 1 番組だけ復号します。最初の SID %d だけを出力します", v))
	}
	return strconv.Itoa(v), nil, warn, nil
}

// parseSID は 10 進または 0x 付き 16 進の SID（1〜65535）を解釈する。
func parseSID(s string) (int, error) {
	digits, base := s, 10
	if strings.HasPrefix(s, "0x") {
		digits, base = s[2:], 16
	}
	v, err := strconv.ParseUint(digits, base, 16)
	if err != nil || v == 0 {
		return 0, fmt.Errorf("--sid の値 %q を解釈できません（SID / hd / all）", s)
	}
	return int(v), nil
}

// Build はコマンドラインと設定から実行内容を組み立てる。
func Build(o *Options, cfg *config.Config, d *config.Device) (*Plan, error) {
	ch, err := channel.Parse(o.Channel)
	if err != nil {
		return nil, err
	}
	if d.Family == config.FamilyDTH30 && ch.Satellite() {
		return nil, fmt.Errorf("デバイス %s（DTH30 系）は地デジ専用です", d.Name)
	}
	p := &Plan{Device: d, Channel: ch}
	mode, warn, err := ResolveMode(o, d, ch)
	if err != nil {
		return nil, err
	}
	p.Mode = mode
	p.Warnings = append(p.Warnings, warn...)

	prog, raw, warn, err := ResolveSID(mode, o.SID, cfg.SidAll)
	if err != nil {
		return nil, err
	}
	p.Program, p.RawSIDs = prog, raw
	p.Warnings = append(p.Warnings, warn...)
	if len(raw) > 0 {
		p.Notes = append(p.Notes, fmt.Sprintf("raw モードの --sid %s は受け取りましたが、TS フィルターは未実装のため全番組の TS を出力します", strings.Join(raw, ",")))
	}

	dth, notes, warn := dthArgv(o, cfg, d, ch, mode, prog)
	p.Stages = [][]string{dth}
	p.Notes = append(p.Notes, notes...)
	p.Warnings = append(p.Warnings, warn...)

	switch {
	case o.B25 && mode == ModeRaw:
		b25 := cfg.B25CmdFor(d)
		if len(b25) == 0 {
			return nil, errors.New("--b25 には設定ファイルの b25_cmd（外部 B25 復号コマンド）が必要です")
		}
		p.Stages = append(p.Stages, append([]string(nil), b25...))
		p.Notes = append(p.Notes, "raw モードの TS を外部 B25 復号コマンドに通します: "+strings.Join(b25, " "))
	case o.B25:
		p.Notes = append(p.Notes, "device モードは内蔵 B-CAS で復号済みのため、外部 B25 復号は通さずそのまま出力します")
	case mode == ModeRaw:
		p.Notes = append(p.Notes, "raw モードの出力はスクランブルされたままです（復号するには --b25 か録画サーバー側のデコーダーを使ってください）")
	}

	if !o.Unlimited {
		p.Limit = o.Duration
		if d.Connection == config.ConnDirect {
			// dth tune が -t で自分で止まる。recdth の計時は保険。
			p.Limit = o.Duration + directWatchdog
		}
	}
	return p, nil
}

// dthArgv は dth のコマンドラインを組み立てる。
func dthArgv(o *Options, cfg *config.Config, d *config.Device, ch channel.Channel, mode Mode, prog string) (argv, notes, warn []string) {
	argv = []string{cfg.DthBinFor(d)}
	for i := 1; i < o.Verbose; i++ {
		argv = append(argv, "-v")
	}
	lnb := d.Lnb
	if o.LNB != nil {
		lnb = *o.LNB
	}
	if d.Connection == config.ConnServe {
		argv = append(argv, "stream", "--socket", d.Socket, "-c", ch.Name, "--mode", string(mode))
		if mode == ModeDevice {
			argv = append(argv, "--program", prog)
		}
		if d.Serial != "" {
			argv = append(argv, "--serial", d.Serial)
		}
		if d.Tuner != "" && !ch.Satellite() {
			argv = append(argv, "--tuner", d.Tuner)
		}
		if !d.Takeover {
			argv = append(argv, "--if-free")
		}
		// dth stream には -t / -o / --lnb / --app-keys / --firmware が無い。時間と出力先は recdth が扱う。
		if o.LNB != nil && lnb != 0 {
			warn = append(warn, "serve 接続では --lnb は効きません（dth serve の --lnb で設定してください）")
		}
		return argv, notes, warn
	}
	argv = append(argv, "tune", "-c", ch.Name, "--mode", string(mode))
	if mode == ModeDevice {
		argv = append(argv, "--program", prog)
	}
	secs := 0.0
	if !o.Unlimited {
		secs = o.Duration.Seconds()
	}
	argv = append(argv, "-t", strconv.FormatFloat(secs, 'f', -1, 64))
	if d.Serial != "" {
		argv = append(argv, "--serial", d.Serial)
	}
	if mode == ModeDevice && d.AppKeys != "" && d.Family != config.FamilyDTH30 {
		argv = append(argv, "--app-keys", d.AppKeys)
	}
	if d.Firmware != "" && d.Family != config.FamilyDTH10 {
		argv = append(argv, "--firmware", d.Firmware)
	}
	if d.Tuner != "" && !ch.Satellite() {
		argv = append(argv, "--tuner", d.Tuner)
	}
	if ch.Satellite() && lnb != 0 {
		argv = append(argv, "--lnb", strconv.Itoa(lnb))
	}
	return argv, notes, warn
}
