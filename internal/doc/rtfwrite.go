package doc

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg" // image sizes
	_ "image/png"
	"math"
	"strings"
	"unicode/utf16"
)

// Styles used for the Markdown elements in rich text. They are what the
// RTF reader recognises again.
var (
	codeBackground = Color{0xF0, 0xF0, 0xF0}
	quoteColor     = Color{0x59, 0x59, 0x59}
	ruleColor      = Color{0xA0, 0xA0, 0xA0}
)

// RuleText is the text of a horizontal rule in rich text, which has no
// rules of its own.
var RuleText = strings.Repeat("─", 40)

const twip = 20.0 // twips per point

// FieldLinks writes links as HYPERLINK fields. Wine's RichEdit drops the
// text of fields, so under Wine links are written as underlined text.
var FieldLinks = true

func twips(pt float64) int { return int(math.Round(pt * twip)) }

// List indents: level L has its text at 0.5" * (L+1) and the marker 0.25"
// to the left of it, as WordPad does for level 0. The Increase Indent
// button adds 0.5", which is one level.
const (
	listStep    = 36.0
	listHanging = 18.0
)

type rtfWriter struct {
	fonts  []string
	colors []Color
}

func (w *rtfWriter) font(name string) int {
	if name == "" {
		name = DefaultFont
	}
	for i, f := range w.fonts {
		if strings.EqualFold(f, name) {
			return i
		}
	}
	w.fonts = append(w.fonts, name)
	return len(w.fonts) - 1
}

// color returns the index in the colour table; 0 is "auto".
func (w *rtfWriter) color(c Color) int {
	for i, x := range w.colors {
		if x == c {
			return i + 1
		}
	}
	w.colors = append(w.colors, c)
	return len(w.colors)
}

// WriteRTF converts a document to RTF.
func WriteRTF(d *Document) []byte {
	w := &rtfWriter{}
	w.font(DefaultFont)
	w.font("Symbol")
	var body bytes.Buffer
	w.blocks(&body, d.Blocks)

	var out bytes.Buffer
	out.WriteString(`{\rtf1\ansi\ansicpg1252\deff0\nouicompat\deflang1033{\fonttbl`)
	for i, f := range w.fonts {
		charset := 0
		if f == "Symbol" {
			charset = 2
		}
		fmt.Fprintf(&out, `{\f%d\fnil\fcharset%d %s;}`, i, charset, escapeText(f))
	}
	out.WriteString("}\r\n")
	if len(w.colors) > 0 {
		out.WriteString(`{\colortbl ;`)
		for _, c := range w.colors {
			fmt.Fprintf(&out, `\red%d\green%d\blue%d;`, c.R, c.G, c.B)
		}
		out.WriteString("}\r\n")
	}
	out.WriteString(`\viewkind4\uc1` + "\r\n")
	out.Write(body.Bytes())
	out.WriteString("}\r\n")
	return out.Bytes()
}

func (w *rtfWriter) blocks(b *bytes.Buffer, blocks []Block) {
	nums := Numbering(blocks)
	for _, blk := range blocks {
		switch v := blk.(type) {
		case *Paragraph:
			w.paragraph(b, v, nums[v], false)
			b.WriteString(`\par` + "\r\n")
		case *Table:
			w.table(b, v)
		}
	}
}

// paragraph writes the properties and text of a paragraph, without the
// \par or \cell that ends it.
func (w *rtfWriter) paragraph(b *bytes.Buffer, p *Paragraph, number int, inTable bool) {
	if p.PageBreakBefore && !inTable {
		b.WriteString(`\page` + "\r\n")
	}
	b.WriteString(`\pard`)
	if inTable {
		b.WriteString(`\intbl`)
	}
	left, first := p.LeftIndent, p.FirstIndent
	before, after := p.SpaceBefore, p.SpaceAfter
	align := p.Align
	switch {
	case p.List != nil:
		if left == 0 {
			left = listStep * float64(p.List.Level+1)
		}
		if first == 0 {
			first = -listHanging
		}
	case p.Quote:
		if left == 0 {
			left = listStep / 2
		}
	case p.Rule:
		align = AlignCenter
	}
	if p.Heading > 0 && before == 0 && after == 0 {
		before, after = 12, 3
	}
	if p.List != nil {
		marker := Marker(p.List, number)
		if p.List.Ordered {
			format := []string{`\pndec`, `\pnlcltr`, `\pnucltr`, `\pnlcrm`, `\pnucrm`}[p.List.Format]
			start := max(1, number)
			fmt.Fprintf(b, `{\pntext\f0 %s\tab}{\*\pn\pnlvlbody\pnf0\pnindent0\pnstart%d%s{\pntxta.}}`,
				escapeText(marker), start, format)
		} else {
			fmt.Fprintf(b, `{\pntext\f0 \u8226?\tab}{\*\pn\pnlvlblt\pnf0\pnindent0{\pntxtb\u8226?}}`)
		}
	}
	switch align {
	case AlignCenter:
		b.WriteString(`\qc`)
	case AlignRight:
		b.WriteString(`\qr`)
	case AlignJustify:
		b.WriteString(`\qj`)
	}
	if left != 0 {
		fmt.Fprintf(b, `\li%d`, twips(left))
	}
	if p.RightIndent != 0 {
		fmt.Fprintf(b, `\ri%d`, twips(p.RightIndent))
	}
	if first != 0 {
		fmt.Fprintf(b, `\fi%d`, twips(first))
	}
	if before != 0 {
		fmt.Fprintf(b, `\sb%d`, twips(before))
	}
	fmt.Fprintf(b, `\sa%d`, twips(after))
	if p.LineSpacing > 0 && p.LineSpacing != 1 {
		fmt.Fprintf(b, `\sl%d\slmult1`, int(math.Round(240*p.LineSpacing)))
	}
	if p.Heading > 0 {
		fmt.Fprintf(b, `\outlinelevel%d`, p.Heading-1)
	}
	b.WriteString(" ")

	if p.Rule {
		w.run(b, &Run{Text: RuleText, Color: &ruleColor})
		return
	}
	for i := range p.Runs {
		r := p.Runs[i] // a copy, styled for the paragraph
		if p.Heading > 0 {
			r.Bold = true
			if r.Size == 0 {
				r.Size = HeadingSize(p.Heading)
			}
		}
		if p.Code {
			r.Font = CodeFont
			if r.Size == 0 {
				r.Size = CodeSize
			}
			if r.Highlight == nil {
				r.Highlight = &codeBackground
			}
		}
		if p.Quote && r.Color == nil {
			r.Color = &quoteColor
		}
		w.run(b, &r)
	}
}

// run writes a run as a group.
func (w *rtfWriter) run(b *bytes.Buffer, r *Run) {
	if r.Image != nil {
		w.image(b, r.Image)
		return
	}
	if r.Text == "" {
		return
	}
	field := r.Link != "" && FieldLinks
	if field {
		fmt.Fprintf(b, `{\field{\*\fldinst{HYPERLINK "%s"}}{\fldrslt`, escapeText(strings.ReplaceAll(r.Link, `"`, "%22")))
	}
	// Every run sets all of its formatting: some readers (Wine's RichEdit)
	// do not restore it at the end of a group.
	size := r.Size
	if size <= 0 {
		size = DefaultSize
	}
	fmt.Fprintf(b, `{\plain\f%d\fs%d`, w.font(r.Font), int(math.Round(size*2)))
	if r.Link != "" && !FieldLinks {
		b.WriteString(`\ul`)
		if r.Color == nil {
			fmt.Fprintf(b, `\cf%d`, w.color(Color{0x05, 0x63, 0xC1}))
		}
	}
	if r.Bold {
		b.WriteString(`\b`)
	}
	if r.Italic {
		b.WriteString(`\i`)
	}
	if r.Underline {
		b.WriteString(`\ul`)
	}
	if r.Strike {
		b.WriteString(`\strike`)
	}
	if r.Super {
		b.WriteString(`\super`)
	} else if r.Sub {
		b.WriteString(`\sub`)
	}
	if r.Color != nil {
		fmt.Fprintf(b, `\cf%d`, w.color(*r.Color))
	}
	if r.Highlight != nil {
		fmt.Fprintf(b, `\highlight%d`, w.color(*r.Highlight))
	}
	b.WriteString(" ") // ends the last control word
	b.WriteString(escapeText(r.Text))
	b.WriteString("}")
	if field {
		b.WriteString("}}")
	}
}

// image writes a picture group with the size in twips.
func (w *rtfWriter) image(b *bytes.Buffer, img *Image) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil {
		return
	}
	blip := `\pngblip`
	if img.Format == "jpeg" {
		blip = `\jpegblip`
	}
	width, height := img.Width, img.Height
	if width <= 0 || height <= 0 {
		width, height = float64(cfg.Width)*0.75, float64(cfg.Height)*0.75 // 96 dpi
	}
	fmt.Fprintf(b, `{\pict%s\picw%d\pich%d\picwgoal%d\pichgoal%d`+"\r\n", blip, cfg.Width, cfg.Height, twips(width), twips(height))
	h := hex.EncodeToString(img.Data)
	for len(h) > 128 {
		b.WriteString(h[:128])
		b.WriteString("\r\n")
		h = h[128:]
	}
	b.WriteString(h)
	b.WriteString("}")
}

// table writes a table. Cell widths come from the table or are shared out
// over 6.5", the text width of a Letter page with 1" margins.
func (w *rtfWriter) table(b *bytes.Buffer, t *Table) {
	cols := t.Columns()
	if cols == 0 {
		return
	}
	widths := make([]float64, cols)
	total := 0.0
	for i := range widths {
		if i < len(t.ColWidths) && t.ColWidths[i] > 0 {
			widths[i] = t.ColWidths[i]
		} else {
			widths[i] = 468.0 / float64(cols)
		}
		total += widths[i]
	}
	const border = `\clbrdrt\brdrw10\brdrs\clbrdrl\brdrw10\brdrs\clbrdrb\brdrw10\brdrs\clbrdrr\brdrw10\brdrs`
	for ri, row := range t.Rows {
		b.WriteString(`\trowd\trgaph108\trleft0`)
		if ri == 0 && t.Header {
			b.WriteString(`\trhdr`)
		}
		x, col := 0.0, 0
		for _, c := range row.Cells {
			for s := 0; s < max(1, c.Span) && col < cols; s++ {
				x += widths[col]
				col++
			}
			b.WriteString(border)
			fmt.Fprintf(b, `\cellx%d`, twips(x))
		}
		b.WriteString("\r\n")
		for ci, c := range row.Cells {
			blocks := c.Blocks
			if len(blocks) == 0 {
				blocks = []Block{&Paragraph{}}
			}
			var paras []*Paragraph
			flattenParagraphs(blocks, &paras)
			for pi, p := range paras {
				q := *p
				if ri == 0 && t.Header {
					runs := make([]Run, len(q.Runs))
					for k, r := range q.Runs {
						r.Bold = true
						runs[k] = r
					}
					q.Runs = runs
				}
				if q.Align == AlignLeft && ci < len(t.Aligns) {
					q.Align = t.Aligns[ci]
				}
				w.paragraph(b, &q, 0, true)
				if pi < len(paras)-1 {
					b.WriteString(`\par` + "\r\n")
				}
			}
			b.WriteString(`\cell` + "\r\n")
		}
		b.WriteString(`\row` + "\r\n")
	}
	b.WriteString(`\pard` + "\r\n")
}

// flattenParagraphs collects the paragraphs of blocks, also those of
// nested tables.
func flattenParagraphs(blocks []Block, out *[]*Paragraph) {
	for _, blk := range blocks {
		switch v := blk.(type) {
		case *Paragraph:
			*out = append(*out, v)
		case *Table:
			for _, row := range v.Rows {
				for _, c := range row.Cells {
					flattenParagraphs(c.Blocks, out)
				}
			}
		}
	}
}

// escapeText escapes text for RTF: the special characters, tabs and line
// breaks, and everything outside ASCII as \u with a "?" fallback.
func escapeText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\' || r == '{' || r == '}':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\tab `)
		case r == '\n':
			b.WriteString(`\line `)
		case r == '\r':
		case r < 0x20:
		case r < 0x80:
			b.WriteRune(r)
		case r <= 0xFFFF:
			fmt.Fprintf(&b, `\u%d?`, int16(r))
		default:
			for _, u := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&b, `\u%d?`, int16(u))
			}
		}
	}
	return b.String()
}

// InlineRTF makes RTF of runs without a paragraph end, for inserting into
// the text at the caret.
func InlineRTF(runs []Run) []byte {
	w := &rtfWriter{}
	w.font(DefaultFont)
	w.font("Symbol")
	var body bytes.Buffer
	for i := range runs {
		w.run(&body, &runs[i])
	}
	var out bytes.Buffer
	out.WriteString(`{\rtf1\ansi\ansicpg1252\deff0{\fonttbl`)
	for i, f := range w.fonts {
		fmt.Fprintf(&out, `{\f%d\fnil\fcharset0 %s;}`, i, escapeText(f))
	}
	out.WriteString("}")
	if len(w.colors) > 0 {
		out.WriteString(`{\colortbl ;`)
		for _, c := range w.colors {
			fmt.Fprintf(&out, `\red%d\green%d\blue%d;`, c.R, c.G, c.B)
		}
		out.WriteString("}")
	}
	out.WriteString(`\uc1 `)
	out.Write(body.Bytes())
	out.WriteString("}")
	return out.Bytes()
}
