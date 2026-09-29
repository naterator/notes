package theme

import (
	"math"
	"strconv"
	"testing"
)

func luminance(hex string) float64 {
	v, _ := strconv.ParseUint(hex[1:], 16, 32)
	var c [3]float64
	for i, shift := range []uint{16, 8, 0} {
		c[i] = float64((v>>shift)&255) / 255
		if c[i] <= 0.04045 {
			c[i] /= 12.92
		} else {
			c[i] = math.Pow((c[i]+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}
func contrast(a, b string) float64 {
	x, y := luminance(a), luminance(b)
	return (math.Max(x, y) + 0.05) / (math.Min(x, y) + 0.05)
}
func TestPaletteReadability(t *testing.T) {
	if len(List()) != 18 {
		t.Fatal("expected eighteen schemes")
	}
	seen := map[string]bool{}
	for _, s := range List() {
		if seen[s.ID] {
			t.Fatal("duplicate scheme", s.ID)
		}
		seen[s.ID] = true
		p := s.Palette
		if s.Kind == "terminal" {
			if p.Base != "" || p.Text != "" {
				t.Fatal("terminal has fixed colors")
			}
			continue
		}
		for _, hex := range []string{p.Base, p.Surface, p.Text, p.Muted, p.Border, p.Selection, p.SelectionText, p.Accent, p.Success, p.Warning, p.Error} {
			if len(hex) != 7 || hex[0] != '#' {
				t.Fatalf("%s: invalid color %q", s.ID, hex)
			}
			if _, err := strconv.ParseUint(hex[1:], 16, 32); err != nil {
				t.Fatal(err)
			}
		}
		for _, pair := range [][2]string{{p.Text, p.Base}, {p.Text, p.Surface}, {p.SelectionText, p.Selection}} {
			if ratio := contrast(pair[0], pair[1]); ratio < 4.5 {
				t.Errorf("%s %v contrast %.2f", s.ID, pair, ratio)
			}
		}
	}
}
