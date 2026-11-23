package icons

import (
	"image/color"
	"testing"
)

func TestAllIconsRender(t *testing.T) {
	names := Names()
	if len(names) < 40 {
		t.Fatalf("expected the toolbar icons, got %v", names)
	}
	for _, size := range []int{16, 20, 32, 40} {
		for _, name := range names {
			img, err := Render(name, size, color.RGBA{0x20, 0x40, 0x60, 0xff})
			if err != nil {
				t.Fatal(err)
			}
			visible := 0
			for i := 3; i < len(img.Pix); i += 4 {
				if img.Pix[i] >= 0xa0 {
					visible++
				}
			}
			if visible == 0 {
				t.Fatalf("%s at %dpx is empty", name, size)
			}
		}
	}
}

func TestAppIcon(t *testing.T) {
	img := AppIcon(48)
	if c := img.RGBAAt(24, 2); c != Background {
		t.Fatalf("edge colour %v", c)
	}
	if c := img.RGBAAt(0, 0); c.A != 0 {
		t.Fatalf("corner is not transparent: %v", c)
	}
}
