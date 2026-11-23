package pdf

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/mevdschee/word4win/internal/doc"
	"github.com/mevdschee/word4win/internal/fonts"
	"github.com/mevdschee/word4win/internal/layout"
)

func settings(scale float64) layout.Settings {
	return layout.Settings{PageWidth: layout.A4[0], PageHeight: layout.A4[1],
		MarginLeft: 72, MarginTop: 72, MarginRight: 72, MarginBottom: 72, Scale: scale}
}

func TestPDF(t *testing.T) {
	if fonts.System().Face("Arial", false, false).Font == nil {
		t.Skip("no fonts installed")
	}
	data, err := os.ReadFile("../doc/testdata/sample.md")
	if err != nil {
		t.Fatal(err)
	}
	d := doc.ReadMarkdown(data, "../doc/testdata")
	pages := layout.Layout(d, settings(1))
	if len(pages) != 1 {
		t.Fatalf("%d pages", len(pages))
	}
	out, err := Write(pages, "sample")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) || !bytes.Contains(out, []byte("/Image")) || !bytes.Contains(out, []byte("FontFile2")) {
		t.Errorf("not a PDF with a picture and an embedded font (%d bytes)", len(out))
	}
}

func TestScale(t *testing.T) {
	if fonts.System().Face("Arial", false, false).Font == nil {
		t.Skip("no fonts installed")
	}
	d := doc.ReadMarkdown([]byte(strings.Repeat("A paragraph of text that wraps over a line or two when the page is narrow enough to need it.\n\n", 120)), "")
	small, normal, big := len(layout.Layout(d, settings(0.5))), len(layout.Layout(d, settings(1))), len(layout.Layout(d, settings(2)))
	if !(small < normal && normal < big) {
		t.Errorf("pages at 50%%, 100%%, 200%%: %d, %d, %d", small, normal, big)
	}
}
