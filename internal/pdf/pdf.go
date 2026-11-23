// Package pdf writes laid out pages as a PDF file, with the TrueType fonts
// embedded (as subsets), the pictures and the links.
package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/mevdschee/word4win/internal/fonts"
	"github.com/mevdschee/word4win/internal/layout"
)

// Write makes a PDF of pages.
func Write(pages []*layout.Page, title string) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("no pages")
	}
	f := fpdf.NewCustom(&fpdf.InitType{UnitStr: "pt", Size: fpdf.SizeType{Wd: pages[0].Width, Ht: pages[0].Height}})
	f.SetAutoPageBreak(false, 0)
	f.SetMargins(0, 0, 0)
	f.SetCompression(true)
	f.SetCreator("word4win", true)
	if title != "" {
		f.SetTitle(title, true)
	}
	w := &writer{f: f, fonts: map[*fonts.Font]string{}, images: map[any]string{}}
	for _, p := range pages {
		orient := "P"
		if p.Width > p.Height {
			orient = "L"
		}
		f.AddPageFormat(orient, fpdf.SizeType{Wd: p.Width, Ht: p.Height})
		for _, it := range p.Items {
			w.item(it)
		}
		if err := f.Error(); err != nil {
			return nil, err
		}
	}
	var b bytes.Buffer
	if err := f.Output(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

type writer struct {
	f      *fpdf.Fpdf
	fonts  map[*fonts.Font]string
	images map[any]string
}

func (w *writer) font(fn *fonts.Font) (string, bool) {
	if fn == nil {
		return "", false
	}
	if name, ok := w.fonts[fn]; ok {
		return name, name != ""
	}
	name := fmt.Sprintf("F%d", len(w.fonts)+1)
	w.f.AddUTF8FontFromBytes(name, "", fn.Data())
	if w.f.Err() {
		w.f.ClearError() // a font that cannot be embedded: its text is left out
		w.fonts[fn] = ""
		return "", false
	}
	w.fonts[fn] = name
	return name, true
}

func (w *writer) item(it layout.Item) {
	f := w.f
	switch v := it.(type) {
	case *layout.Rect:
		f.SetFillColor(int(v.Fill.R), int(v.Fill.G), int(v.Fill.B))
		f.Rect(v.X, v.Y, v.W, v.H, "F")
	case *layout.Line:
		f.SetDrawColor(int(v.Color.R), int(v.Color.G), int(v.Color.B))
		f.SetLineWidth(v.Width)
		f.Line(v.X1, v.Y1, v.X2, v.Y2)
	case *layout.Text:
		name, ok := w.font(v.Face.Font)
		if !ok || strings.TrimSpace(v.Text) == "" {
			return
		}
		f.SetFont(name, "", v.Size)
		f.SetTextColor(int(v.Color.R), int(v.Color.G), int(v.Color.B))
		if v.Face.SynthItalic {
			f.TransformBegin()
			f.TransformSkew(-12, 0, v.X, v.Y)
		}
		if v.Face.SynthBold {
			f.SetDrawColor(int(v.Color.R), int(v.Color.G), int(v.Color.B))
			f.SetLineWidth(v.Size * 0.03)
			f.SetTextRenderingMode(2) // fill and stroke
		}
		f.Text(v.X, v.Y, v.Text)
		if v.Face.SynthBold {
			f.SetTextRenderingMode(0)
		}
		if v.Face.SynthItalic {
			f.TransformEnd()
		}
	case *layout.Picture:
		name, typ := w.image(v)
		if name == "" {
			return
		}
		f.ImageOptions(name, v.X, v.Y, v.W, v.H, false, fpdf.ImageOptions{ImageType: typ}, 0, "")
	case *layout.Link:
		if strings.HasPrefix(v.URL, "#") {
			return
		}
		f.LinkString(v.X, v.Y, v.W, v.H, v.URL)
	}
}

// image registers a picture once. PNG files that the PDF writer cannot
// take as they are (16 bits, interlaced, palettes with transparency) are
// converted to plain 8-bit PNG first.
func (w *writer) image(p *layout.Picture) (string, string) {
	key := any(p.Image)
	if name, ok := w.images[key]; ok {
		if name == "" {
			return "", ""
		}
		return name, imageType(p.Image.Format)
	}
	name := fmt.Sprintf("I%d", len(w.images)+1)
	data, typ := p.Image.Data, imageType(p.Image.Format)
	if typ == "PNG" && !simplePNG(data) {
		if conv := reencode(data); conv != nil {
			data = conv
		}
	}
	w.f.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: typ}, bytes.NewReader(data))
	if w.f.Err() {
		w.f.ClearError()
		if conv := reencode(p.Image.Data); conv != nil {
			w.f.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(conv))
			typ = "PNG"
		}
		if w.f.Err() {
			w.f.ClearError()
			w.images[key] = ""
			return "", ""
		}
	}
	w.images[key] = name
	return name, typ
}

func imageType(format string) string {
	if format == "jpeg" {
		return "JPG"
	}
	return "PNG"
}

// simplePNG reports whether a PNG has 8 bits per channel and no
// interlacing.
func simplePNG(data []byte) bool {
	if len(data) < 29 {
		return false
	}
	depth, ctype, interlace := data[24], data[25], data[28]
	return depth == 8 && interlace == 0 && ctype != 3
}

func reencode(data []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	rgba := image.NewNRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	var b bytes.Buffer
	if png.Encode(&b, rgba) != nil {
		return nil
	}
	return b.Bytes()
}
