package fonts

import "testing"

func TestSystemFonts(t *testing.T) {
	l := System()
	face := l.Face("Calibri", false, false)
	if face.Font == nil {
		t.Skip("no fonts installed")
	}
	f := face.Font
	if f.UnitsPerEm == 0 || f.Ascent <= 0 || f.Descent <= 0 {
		t.Fatalf("metrics of %s: %+v", f.Family, f)
	}
	if w := f.Advance('M'); w <= 0.3 || w > 1.5 {
		t.Errorf("advance of M in %s: %v", f.Family, w)
	}
	if !f.HasGlyph('a') {
		t.Errorf("%s has no 'a'", f.Family)
	}
	b := l.Face("Calibri", true, true)
	t.Logf("Calibri -> %s %s; bold italic -> %s %s synth %v %v", f.Family, f.Subfamily, b.Font.Family, b.Font.Subfamily, b.SynthBold, b.SynthItalic)
	if fb, ok := l.Fallback('漢'); ok {
		t.Logf("fallback for 漢: %s", fb.Family)
	}
}
