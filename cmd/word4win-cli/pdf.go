package main

import (
	"strings"

	"github.com/mevdschee/word4win/internal/doc"
	"github.com/mevdschee/word4win/internal/layout"
	"github.com/mevdschee/word4win/internal/pdf"
)

func writePDF(d *doc.Document, scale int, paper string) ([]byte, error) {
	size := layout.A4
	if strings.EqualFold(paper, "letter") {
		size = layout.Letter
	}
	pages := layout.Layout(d, layout.Settings{
		PageWidth: size[0], PageHeight: size[1],
		MarginLeft: 72, MarginRight: 72, MarginTop: 72, MarginBottom: 72,
		Scale: float64(scale) / 100,
	})
	return pdf.Write(pages, "")
}
