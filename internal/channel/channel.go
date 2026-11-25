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
		return Channel{Type: BS, Name: fmt.Sprintf("BS%02d_%d", tp, slot)}, nil
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
