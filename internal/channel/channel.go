// Package channel は recdth に渡されたチャンネル名を dth の -c に渡す名前へ正規化する。
//
//   - 地デジ: 13〜62（末尾の "ch" は許可）→ "27"
//   - BS: BS15_0 / BS1_0 / BS01 / BS15（相対 TS 番号の省略は 0）→ "BS15_0", "BS01_0"
//   - CS: CS2〜CS24 / ND2〜ND24 / ND02 → "ND02"（相対 TS 番号が 0 以外なら "ND02_1"）
//
// dth 側（dth-common/src/channels.rs）は BS が奇数 1〜23、ND が偶数 2〜24 のトランスポンダーを受け付ける。
package channel

import (
	"fmt"
	"strconv"
	"strings"
)

// Type は放送の種別。Mirakurun の types と同じ表記。
type Type string

const (
	GR Type = "GR"
	BS Type = "BS"
	CS Type = "CS"
)

// Channel は正規化済みのチャンネル。
type Channel struct {
	Type Type
	// Name は dth の -c にそのまま渡す名前。
	Name string
	// TSID は BS チャンネルに対応する既知の TSID（0 なら未定義）。
	TSID uint16
}

// bsTSIDMap は BS の既知の TSID（recisdb 1.3.2 同梱 dvbv5_channels_isdbs.conf の値、2026-11 時点）。
// dth はロック直後に TMCC の TSID 表が全部 0 で返ることがあり、そのときの計算値
// (0x4000 | tp<<4 | slot) が再編後の TSID と食い違うため、--tsid で明示して防ぐ。
var bsTSIDMap = map[string]uint16{
	"BS01_0": 0x4010, // BS朝日
	"BS01_1": 0x4011, // BS-TBS
	"BS01_2": 0x4012, // BSテレ東
	"BS03_0": 0x4030, // WOWOWプライム
	"BS03_1": 0x4631,
	"BS03_2": 0x4632,
	"BS05_0": 0x4450, // WOWOWライブ
	"BS05_1": 0x4451, // WOWOWシネマ
	"BS07_0": 0x4470, // スターチャンネル
	"BS09_0": 0x4090, // BS11
	"BS09_1": 0x4092, // BS12 トゥエルビ
	"BS11_0": 0x46b0,
	"BS11_1": 0x46b1,
	"BS11_2": 0x46b2,
	"BS13_0": 0x40d0, // BS日テレ
	"BS13_1": 0x40d1, // BSフジ
	"BS13_2": 0x46d2, // 放送大学
	"BS15_0": 0x40f1, // NHK BS
	"BS15_1": 0x40f2,
	"BS15_2": 0x48f3, // BS10
	"BS17_0": 0x4310,
	"BS19_0": 0x4730, // J SPORTS
	"BS19_1": 0x4731,
	"BS19_2": 0x4732,
	"BS19_3": 0x4733,
	"BS21_0": 0x4750,
	"BS21_1": 0x4751,
	"BS21_2": 0x4752,
	"BS23_0": 0x4770,
	"BS23_1": 0x4971, // BSよしもと
	"BS23_2": 0x4973, // BS松竹東急 / J:COM BS 等
}

// Satellite は BS/CS（ISDB-S）なら true。
func (c Channel) Satellite() bool { return c.Type == BS || c.Type == CS }

const (
	uhfFirst = 13
	uhfLast  = 62
	maxSlot  = 7
)

// Parse はチャンネル名を正規化する。大文字小文字は区別しない。
func Parse(s string) (Channel, error) {
	in := strings.TrimSpace(s)
	u := strings.ToUpper(in)
	switch {
	case u == "":
		return Channel{}, fmt.Errorf("チャンネルが空です")
	case strings.HasPrefix(u, "BS"):
		tp, slot, err := splitTpSlot(u[2:])
		if err != nil || tp < 1 || tp > 23 || tp%2 == 0 || slot > maxSlot {
			return Channel{}, fmt.Errorf("BS のチャンネル %q を解釈できません（BS01_0〜BS23_%d、トランスポンダーは奇数）", in, maxSlot)
		}
		name := fmt.Sprintf("BS%02d_%d", tp, slot)
		return Channel{Type: BS, Name: name, TSID: bsTSIDMap[name]}, nil
	case strings.HasPrefix(u, "CS"), strings.HasPrefix(u, "ND"):
		tp, slot, err := splitTpSlot(u[2:])
		if err != nil || tp < 2 || tp > 24 || tp%2 != 0 || slot > maxSlot {
			return Channel{}, fmt.Errorf("CS のチャンネル %q を解釈できません（CS2〜CS24 または ND02〜ND24、偶数）", in)
		}
		name := fmt.Sprintf("ND%02d", tp)
		if slot != 0 {
			name += "_" + strconv.Itoa(slot)
		}
		return Channel{Type: CS, Name: name}, nil
	default:
		n, err := atoiDigits(strings.TrimSuffix(u, "CH"))
		if err != nil || n < uhfFirst || n > uhfLast {
			return Channel{}, fmt.Errorf("地デジのチャンネル %q を解釈できません（%d〜%d）", in, uhfFirst, uhfLast)
		}
		return Channel{Type: GR, Name: strconv.Itoa(n)}, nil
	}
}

// splitTpSlot は "15" / "15_1" をトランスポンダー番号と相対 TS 番号に分ける。
func splitTpSlot(s string) (tp, slot int, err error) {
	tpStr, slotStr, found := strings.Cut(s, "_")
	if tp, err = atoiDigits(tpStr); err != nil {
		return 0, 0, err
	}
	if !found {
		return tp, 0, nil
	}
	if slot, err = atoiDigits(slotStr); err != nil {
		return 0, 0, err
	}
	return tp, slot, nil
}

// atoiDigits は数字だけからなる文字列を整数にする（strconv.Atoi が許す符号は拒否する）。
func atoiDigits(s string) (int, error) {
	if s == "" || len(s) > 4 {
		return 0, fmt.Errorf("数字ではありません: %q", s)
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("数字ではありません: %q", s)
		}
	}
	return strconv.Atoi(s)
}
