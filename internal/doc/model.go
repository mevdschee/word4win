// Package doc is the document model of word4win and its file formats. The
// editor itself works in RTF (the native format of the RichEdit control);
// this model sits between RTF and the other formats: Markdown is read and
// written, DOCX and ODT are read, and the layout engine paginates the model
// for print preview, printing and PDF.
//
// All lengths are in points (1/72 inch).
package doc

import (
	"strings"
)

// Document is a list of blocks.
type Document struct {
	Blocks []Block
}

// Block is a *Paragraph or a *Table.
type Block interface{ isBlock() }

// Align is the horizontal alignment of a paragraph.
type Align int

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
	AlignJustify
)

// NumberFormat is how the items of an ordered list are numbered.
type NumberFormat int

const (
	Decimal NumberFormat = iota
	LowerAlpha
	UpperAlpha
	LowerRoman
	UpperRoman
)

// List marks a paragraph as a list item.
type List struct {
	Ordered bool
	Format  NumberFormat // for ordered lists
	Level   int          // 0 is the outer list
	Start   int          // the number of the first item of an ordered list, set on that item; 0 continues
}

// Paragraph is a block of text with one set of paragraph properties.
type Paragraph struct {
	Runs []Run

	Align                                Align
	LeftIndent, RightIndent, FirstIndent float64 // FirstIndent is relative to LeftIndent, negative for a hanging indent
	SpaceBefore, SpaceAfter              float64
	LineSpacing                          float64 // a multiple of single spacing; 0 is single

	Heading         int // 1 to 6, 0 for body text
	List            *List
	Quote           bool
	Code            bool // a line of a code block
	Rule            bool // a horizontal rule; it has no text
	PageBreakBefore bool
}

func (*Paragraph) isBlock() {}

// Color is an RGB colour.
type Color struct{ R, G, B uint8 }

// Run is text with one set of character properties, or an inline image.
type Run struct {
	Text                            string
	Font                            string  // empty for the default font
	Size                            float64 // points, 0 for the default size
	Bold, Italic, Underline, Strike bool
	Super, Sub                      bool
	Color, Highlight                *Color // nil for automatic and none
	Link                            string
	Image                           *Image
}

// Image is a picture in PNG or JPEG format.
type Image struct {
	Data          []byte
	Format        string  // "png" or "jpeg"
	Width, Height float64 // points
	Alt           string
}

// Table is a grid of cells.
type Table struct {
	Rows      []Row
	ColWidths []float64 // points; may be empty, then the columns share the width
	Header    bool      // the first row is a header row
	Aligns    []Align   // per column, from Markdown
}

func (*Table) isBlock() {}

// Row is a table row.
type Row struct {
	Cells []Cell
}

// Cell is a table cell with its own paragraphs.
type Cell struct {
	Blocks []Block
	Span   int // number of columns it covers; 0 and 1 mean one
}

// Default fonts.
const (
	DefaultFont = "Calibri"
	DefaultSize = 11.0
	CodeFont    = "Consolas"
	CodeSize    = 10.0
)

// HeadingSize is the font size of a heading level. The sizes differ from
// the body size, so that a heading is recognised by its look in rich text
// that has no styles (RichEdit keeps no outline levels).
func HeadingSize(level int) float64 {
	switch level {
	case 1:
		return 24
	case 2:
		return 18
	case 3:
		return 15
	case 4:
		return 13
	case 5:
		return 11.5
	case 6:
		return 10.5
	}
	return DefaultSize
}

// IsMonospace reports whether a font family has fixed-width characters,
// which is how code is recognised in rich text.
func IsMonospace(font string) bool {
	f := strings.ToLower(font)
	for _, m := range []string{"consolas", "courier", "cascadia", "lucida console", "lucida sans typewriter",
		"menlo", "monaco", "dejavu sans mono", "liberation mono", "source code", "fira code", "fira mono",
		"jetbrains mono", "ubuntu mono", "roboto mono", "andale mono", "monospace", "fixedsys", "terminal",
		"ms gothic", "noto mono", "noto sans mono", "sf mono", "hack", "inconsolata"} {
		if strings.Contains(f, m) {
			return true
		}
	}
	return false
}

// Text returns the plain text of a paragraph.
func (p *Paragraph) Text() string {
	var b strings.Builder
	for _, r := range p.Runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

// PlainText returns the text of a document, one line per paragraph and a
// tab between table cells.
func (d *Document) PlainText() string {
	var b strings.Builder
	var blocks func([]Block)
	blocks = func(bs []Block) {
		for _, blk := range bs {
			switch v := blk.(type) {
			case *Paragraph:
				if v.Rule {
					b.WriteString("---\n")
					continue
				}
				b.WriteString(v.Text())
				b.WriteString("\n")
			case *Table:
				for _, row := range v.Rows {
					for i, c := range row.Cells {
						if i > 0 {
							b.WriteString("\t")
						}
						var cb strings.Builder
						for _, cblk := range c.Blocks {
							if p, ok := cblk.(*Paragraph); ok {
								if cb.Len() > 0 {
									cb.WriteString(" ")
								}
								cb.WriteString(p.Text())
							}
						}
						b.WriteString(cb.String())
					}
					b.WriteString("\n")
				}
			}
		}
	}
	blocks(d.Blocks)
	return b.String()
}

// FromPlainText makes a document of plain text, a paragraph per line.
func FromPlainText(s string) *Document {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSuffix(s, "\n")
	d := &Document{}
	for _, line := range strings.Split(s, "\n") {
		p := &Paragraph{}
		if line != "" {
			p.Runs = []Run{{Text: line}}
		}
		d.Blocks = append(d.Blocks, p)
	}
	return d
}

// sameStyle reports whether two runs differ only in their text.
func sameStyle(a, b *Run) bool {
	return a.Font == b.Font && a.Size == b.Size && a.Bold == b.Bold && a.Italic == b.Italic &&
		a.Underline == b.Underline && a.Strike == b.Strike && a.Super == b.Super && a.Sub == b.Sub &&
		sameColor(a.Color, b.Color) && sameColor(a.Highlight, b.Highlight) && a.Link == b.Link &&
		a.Image == nil && b.Image == nil
}

func sameColor(a, b *Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// mergeRuns joins neighbouring runs with the same style and drops empty
// ones.
func mergeRuns(runs []Run) []Run {
	var out []Run
	for _, r := range runs {
		if r.Text == "" && r.Image == nil {
			continue
		}
		if n := len(out); n > 0 && sameStyle(&out[n-1], &r) {
			out[n-1].Text += r.Text
			continue
		}
		out = append(out, r)
	}
	return out
}

// Normalize merges runs in all paragraphs of the document.
func (d *Document) Normalize() {
	var blocks func([]Block)
	blocks = func(bs []Block) {
		for _, blk := range bs {
			switch v := blk.(type) {
			case *Paragraph:
				v.Runs = mergeRuns(v.Runs)
			case *Table:
				for i := range v.Rows {
					for j := range v.Rows[i].Cells {
						blocks(v.Rows[i].Cells[j].Blocks)
					}
				}
			}
		}
	}
	blocks(d.Blocks)
}

// Columns returns the number of columns of a table.
func (t *Table) Columns() int {
	n := len(t.ColWidths)
	for _, r := range t.Rows {
		c := 0
		for _, cell := range r.Cells {
			c += max(1, cell.Span)
		}
		n = max(n, c)
	}
	return n
}

// Marker returns the text of a list item's marker: a bullet or a number.
func Marker(l *List, number int) string {
	if !l.Ordered {
		switch l.Level % 3 {
		case 1:
			return "◦"
		case 2:
			return "▪"
		}
		return "•"
	}
	switch l.Format {
	case LowerAlpha:
		return alpha(number, 'a') + "."
	case UpperAlpha:
		return alpha(number, 'A') + "."
	case LowerRoman:
		return strings.ToLower(roman(number)) + "."
	case UpperRoman:
		return roman(number) + "."
	}
	return itoa(number) + "."
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func alpha(n int, base byte) string {
	if n <= 0 {
		return string(base)
	}
	var b []byte
	for n > 0 {
		n--
		b = append([]byte{base + byte(n%26)}, b...)
		n /= 26
	}
	return string(b)
}

func roman(n int) string {
	if n <= 0 || n >= 4000 {
		return itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// Numbering computes the number of every ordered list item in a list of
// blocks, keyed by paragraph. A list continues over items of deeper levels
// and restarts after any other paragraph, or where Start is set.
func Numbering(blocks []Block) map[*Paragraph]int {
	nums := map[*Paragraph]int{}
	var counters []int
	for _, blk := range blocks {
		p, ok := blk.(*Paragraph)
		if !ok || p.List == nil {
			counters = counters[:0]
			continue
		}
		lvl := p.List.Level
		for len(counters) <= lvl {
			counters = append(counters, 0)
		}
		counters = counters[:lvl+1]
		if p.List.Start > 0 {
			counters[lvl] = p.List.Start
		} else {
			counters[lvl]++
		}
		if p.List.Ordered {
			nums[p] = counters[lvl]
		}
	}
	return nums
}
