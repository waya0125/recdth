package channel

import "testing"

func TestParse(t *testing.T) {
	ok := []struct {
		in   string
		typ  Type
		name string
	}{
		{"13", GR, "13"},
		{"27", GR, "27"},
		{"62", GR, "62"},
		{"27ch", GR, "27"},
		{" 21 ", GR, "21"},
		{"BS15_0", BS, "BS15_0"},
		{"bs15_1", BS, "BS15_1"},
		{"BS1_0", BS, "BS01_0"},
		{"BS01_0", BS, "BS01_0"},
		{"BS15", BS, "BS15_0"},
		{"BS23_3", BS, "BS23_3"},
		{"CS2", CS, "ND02"},
		{"CS24", CS, "ND24"},
		{"cs8", CS, "ND08"},
		{"ND02", CS, "ND02"},
		{"ND2", CS, "ND02"},
		{"ND24", CS, "ND24"},
		{"CS4_0", CS, "ND04"},
		{"ND04_1", CS, "ND04_1"},
	}
	for _, c := range ok {
		got, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got.Type != c.typ || got.Name != c.name {
			t.Errorf("Parse(%q) = %+v, want %s %s", c.in, got, c.typ, c.name)
		}
	}
	bad := []string{"", "12", "63", "0", "-27", "+27", "BS", "BS16_0", "BS0_0", "BS25_0", "BS15_8", "BS15_x", "CS1", "CS3", "CS26", "ND03", "ND", "VHF1", "27.5", "CS-2"}
	for _, in := range bad {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %+v, want error", in, got)
		}
	}
}

func TestSatellite(t *testing.T) {
	for in, want := range map[string]bool{"27": false, "BS15_0": true, "CS2": true} {
		c, err := Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		if c.Satellite() != want {
			t.Errorf("%s Satellite() = %v", in, !want)
		}
	}
}
