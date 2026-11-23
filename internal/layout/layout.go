// Package layout breaks a document into lines and pages. The result is a
// list of pages with positioned text, lines, rectangles and pictures, in
// points from the top left of the page, which the PDF writer and the
// Windows printer (and its preview) draw the same way.
package layout

import (
	"math"
	"unicode"

	"github.com/mevdschee/word4win/internal/doc"
	"github.com/mevdschee/word4win/internal/fonts"
)

// Settings are the page and the scale.
type Settings struct {
	PageWidth, PageHeight                            float64 // points
	MarginLeft, MarginTop, MarginRight, MarginBottom float64
	Scale                                            float64 // 1 is 100%
	Fonts                                            *fonts.Library
}

// Paper sizes in points.
var (
	A4     = [2]float64{595.28, 841.89}
	Letter = [2]float64{612, 792}
)

// Page is a laid out page.
type Page struct {
	Width, Height float64
	Items         []Item
}

// Item is one of *Text, *Rect, *Line, *Picture or *Link.
type Item interface {
	move(dx, dy float64)
	scale(s, ox, oy float64)
}

// Text is a piece of text in one font, drawn from its baseline. Advances
// holds the advance of every character (rune), so that drawing does not
// depend on the device's own measurements.
type Text struct {
	X, Y     float64
	Text     string
	Face     fonts.Face
	Size     float64
	Color    doc.Color
	Advances []float64
}

// Width is the sum of the advances.
func (t *Text) Width() float64 {
	w := 0.0
	for _, a := range t.Advances {
		w += a
	}
	return w
}

// Rect is a filled rectangle.
type Rect struct {
	X, Y, W, H float64
	Fill       doc.Color
}

// Line is a straight line.
type Line struct {
	X1, Y1, X2, Y2 float64
	Width          float64
	Color          doc.Color
}

// Picture is an image in a box.
type Picture struct {
	X, Y, W, H float64
	Image      *doc.Image
}

// Link is a clickable area.
type Link struct {
	X, Y, W, H float64
	URL        string
}

func (t *Text) move(dx, dy float64)    { t.X += dx; t.Y += dy }
func (r *Rect) move(dx, dy float64)    { r.X += dx; r.Y += dy }
func (l *Line) move(dx, dy float64)    { l.X1 += dx; l.X2 += dx; l.Y1 += dy; l.Y2 += dy }
func (p *Picture) move(dx, dy float64) { p.X += dx; p.Y += dy }
func (l *Link) move(dx, dy float64)    { l.X += dx; l.Y += dy }

func (t *Text) scale(s, ox, oy float64) {
	t.X, t.Y, t.Size = ox+t.X*s, oy+t.Y*s, t.Size*s
	for i := range t.Advances {
		t.Advances[i] *= s
	}
}
func (r *Rect) scale(s, ox, oy float64) { r.X, r.Y, r.W, r.H = ox+r.X*s, oy+r.Y*s, r.W*s, r.H*s }
func (l *Line) scale(s, ox, oy float64) {
	l.X1, l.Y1, l.X2, l.Y2, l.Width = ox+l.X1*s, oy+l.Y1*s, ox+l.X2*s, oy+l.Y2*s, l.Width*s
}
func (p *Picture) scale(s, ox, oy float64) { p.X, p.Y, p.W, p.H = ox+p.X*s, oy+p.Y*s, p.W*s, p.H*s }
func (l *Link) scale(s, ox, oy float64)    { l.X, l.Y, l.W, l.H = ox+l.X*s, oy+l.Y*s, l.W*s, l.H*s }

// row is a unit of vertical layout: a line of a paragraph, a table row
// or a rule, with its items relative to its top left.
type row struct {
	h                   float64
	items               []Item
	before, after       float64
	pageBreak, keepNext bool
	header              *row // a table header row to repeat on a new page
}

var (
	black      = doc.Color{}
	linkColor  = doc.Color{R: 0x05, G: 0x63, B: 0xC1}
	quoteColor = doc.Color{R: 0x59, G: 0x59, B: 0x59}
	codeBack   = doc.Color{R: 0xF0, G: 0xF0, B: 0xF0}
	ruleColor  = doc.Color{R: 0xA0, G: 0xA0, B: 0xA0}
	borderCol  = doc.Color{R: 0x80, G: 0x80, B: 0x80}
)

const (
	tabStop     = 36.0
	cellPadX    = 5.0
	cellPadY    = 3.0
	listStep    = 36.0
	listHanging = 18.0
)

type engine struct {
	fonts *fonts.Library
}

// Layout lays out a document on pages.
func Layout(d *doc.Document, s Settings) []*Page {
	if s.Scale <= 0 {
		s.Scale = 1
	}
	if s.Fonts == nil {
		s.Fonts = fonts.System()
	}
	if s.PageWidth <= 0 || s.PageHeight <= 0 {
		s.PageWidth, s.PageHeight = A4[0], A4[1]
	}
	e := &engine{fonts: s.Fonts}
	cw := (s.PageWidth - s.MarginLeft - s.MarginRight) / s.Scale
	ch := (s.PageHeight - s.MarginTop - s.MarginBottom) / s.Scale
	if cw < 36 {
		cw = 36
	}
	if ch < 36 {
		ch = 36
	}
	rows := e.blocks(d.Blocks, cw)

	var pages []*Page
	var page *Page
	y := 0.0
	newPage := func() {
		page = &Page{Width: s.PageWidth, Height: s.PageHeight}
		pages = append(pages, page)
		y = 0
	}
	place := func(r *row, at float64) {
		for _, it := range r.items {
			it.move(0, at)
			it.scale(s.Scale, s.MarginLeft, s.MarginTop)
			page.Items = append(page.Items, it)
		}
	}
	newPage()
	for i, r := range rows {
		if r.pageBreak && y > 0 {
			newPage()
		}
		before := r.before
		if y == 0 {
			before = 0
		}
		need := before + r.h
		if r.keepNext && i+1 < len(rows) { // a heading stays with what follows
			next := rows[i+1]
			need += r.after + next.before + next.h
		}
		if y > 0 && y+need > ch+0.01 {
			newPage()
			before = 0
			if r.header != nil && r.header != r {
				h := cloneRow(r.header)
				place(h, 0)
				y = h.h
			}
		}
		place(r, y+before)
		y += before + r.h + r.after
	}
	return pages
}

// cloneRow copies a row with its items, for repeating table headers.
func cloneRow(r *row) *row {
	c := *r
	c.items = nil
	for _, it := range r.items {
		switch v := it.(type) {
		case *Text:
			t := *v
			t.Advances = append([]float64{}, v.Advances...)
			c.items = append(c.items, &t)
		case *Rect:
			x := *v
			c.items = append(c.items, &x)
		case *Line:
			x := *v
			c.items = append(c.items, &x)
		case *Picture:
			x := *v
			c.items = append(c.items, &x)
		case *Link:
			x := *v
			c.items = append(c.items, &x)
		}
	}
	return &c
}

// blocks lays out blocks in a column of a width.
func (e *engine) blocks(blocks []doc.Block, width float64) []*row {
	nums := doc.Numbering(blocks)
	var rows []*row
	for _, b := range blocks {
		switch v := b.(type) {
		case *doc.Paragraph:
			rows = append(rows, e.paragraph(v, nums[v], width)...)
		case *doc.Table:
			rows = append(rows, e.table(v, width)...)
		}
	}
	return rows
}

// style is the resolved look of a piece of text.
type style struct {
	face              fonts.Face
	size              float64 // the drawn size (smaller for super and subscript)
	shift             float64 // baseline shift, positive is up
	color             doc.Color
	underline, strike bool
	highlight         *doc.Color
	link              string
}

func (s *style) same(o *style) bool {
	return s.face == o.face && s.size == o.size && s.shift == o.shift && s.color == o.color &&
		s.underline == o.underline && s.strike == o.strike && s.link == o.link &&
		(s.highlight == nil) == (o.highlight == nil) && (s.highlight == nil || *s.highlight == *o.highlight)
}

func (e *engine) styleOf(p *doc.Paragraph, r *doc.Run) style {
	size := r.Size
	family := r.Font
	if p.Code {
		family = doc.CodeFont
		if size == 0 {
			size = doc.CodeSize
		}
	}
	if size == 0 {
		size = doc.DefaultSize
		if p.Heading > 0 {
			size = doc.HeadingSize(p.Heading)
		}
	}
	if family == "" {
		family = doc.DefaultFont
	}
	bold := r.Bold || p.Heading > 0
	st := style{face: e.fonts.Face(family, bold, r.Italic), size: size, underline: r.Underline,
		strike: r.Strike, highlight: r.Highlight, link: r.Link}
	if p.Code {
		st.highlight = nil // the code background is drawn for the whole line
	}
	switch {
	case r.Color != nil:
		st.color = *r.Color
	case r.Link != "":
		st.color = linkColor
	case p.Quote:
		st.color = quoteColor
	default:
		st.color = black
	}
	if r.Link != "" {
		st.underline = true
	}
	if r.Super || r.Sub {
		st.size = size * 0.65
		if r.Super {
			st.shift = size * 0.33
		} else {
			st.shift = -size * 0.14
		}
	}
	return st
}

// piece is a word, a space, a tab, a line break or a picture.
type piece struct {
	text       string
	st         style
	w          float64
	advances   []float64
	space      bool
	tab        bool
	brk        bool
	glue       bool // no line break before this piece
	img        *doc.Image
	imgW, imgH float64
}

func (pc *piece) ascent() float64 {
	if pc.img != nil {
		return pc.imgH
	}
	if pc.st.face.Font == nil {
		return pc.st.size * 0.9
	}
	return pc.st.face.Font.AscentEm()*pc.st.size + math.Max(0, pc.st.shift)
}

func (pc *piece) descent() float64 {
	if pc.img != nil {
		return 0
	}
	if pc.st.face.Font == nil {
		return pc.st.size * 0.25
	}
	return pc.st.face.Font.DescentEm()*pc.st.size + math.Max(0, -pc.st.shift)
}

// measure fills in the advances of a text piece, splitting it where the
// font lacks characters and another font has them.
func (e *engine) measure(text string, st style, glue bool) []piece {
	var out []piece
	cur := piece{st: st, glue: glue}
	flush := func() {
		if cur.text != "" {
			out = append(out, cur)
		}
	}
	for _, r := range text {
		f := st.face.Font
		pst := st
		if f != nil && !f.HasGlyph(r) && !unicode.IsSpace(r) && r != 0x00AD {
			if fb, ok := e.fonts.Fallback(r); ok {
				pst.face = fonts.Face{Font: fb, SynthBold: st.face.Font.Bold || st.face.SynthBold, SynthItalic: st.face.Font.Italic || st.face.SynthItalic}
				f = fb
			}
		}
		if pst.face != cur.st.face {
			flush()
			cur = piece{st: pst, glue: len(out) > 0 || glue}
		}
		adv := 0.0
		if f != nil {
			adv = f.Advance(r) * pst.size
		} else {
			adv = pst.size * 0.5
		}
		if pst.face.SynthBold {
			adv += pst.size * 0.02
		}
		cur.text += string(r)
		cur.advances = append(cur.advances, adv)
		cur.w += adv
	}
	flush()
	return out
}

// pieces splits the runs of a paragraph into pieces.
func (e *engine) pieces(p *doc.Paragraph, maxW float64) []piece {
	var out []piece
	for i := range p.Runs {
		r := &p.Runs[i]
		st := e.styleOf(p, r)
		if r.Image != nil {
			w, h := r.Image.Width, r.Image.Height
			if w <= 0 || h <= 0 {
				w, h = 100, 100
			}
			if w > maxW { // pictures shrink to the column
				h, w = h*maxW/w, maxW
			}
			out = append(out, piece{img: r.Image, imgW: w, imgH: h, w: w, st: st})
			continue
		}
		text := r.Text
		word := ""
		flushWord := func() {
			if word != "" {
				glue := len(out) > 0 && !out[len(out)-1].space && !out[len(out)-1].tab && !out[len(out)-1].brk
				out = append(out, e.measure(word, st, glue)...)
				word = ""
			}
		}
		for _, c := range text {
			switch c {
			case ' ', 0x3000:
				flushWord()
				ps := e.measure(string(c), st, false)
				for k := range ps {
					ps[k].space = true
				}
				out = append(out, ps...)
			case '\t':
				flushWord()
				out = append(out, piece{tab: true, st: st})
			case '\n', '\v', ' ':
				flushWord()
				out = append(out, piece{brk: true, st: st})
			case '\f', '\r', 0x00AD:
			default:
				word += string(c)
				// CJK text breaks between any two characters.
				if isCJK(c) {
					flushWord()
				}
			}
		}
		flushWord()
	}
	return out
}

func isCJK(r rune) bool {
	return r >= 0x2E80 && r <= 0x9FFF || r >= 0xAC00 && r <= 0xD7AF || r >= 0xF900 && r <= 0xFAFF || r >= 0xFF00 && r <= 0xFFEF
}

type lineOut struct {
	pieces []piece
	xs     []float64 // x of each piece, from the line start
	width  float64   // without trailing spaces
	last   bool      // the last line of the paragraph or before a line break
	start  float64   // x of the line start in the column
	avail  float64
}

// paragraph lays out a paragraph as rows, one per line.
func (e *engine) paragraph(p *doc.Paragraph, number int, width float64) []*row {
	left, first, right := p.LeftIndent, p.FirstIndent, p.RightIndent
	before, after := p.SpaceBefore, p.SpaceAfter
	if p.List != nil {
		if left == 0 {
			left = listStep * float64(p.List.Level+1)
		}
		if first == 0 {
			first = -listHanging
		}
	} else if p.Quote && left == 0 {
		left = listStep / 2
	}
	if p.Heading > 0 && before == 0 && after == 0 {
		before, after = 12, 3
	}
	spacing := p.LineSpacing
	if spacing <= 0 {
		spacing = 1
	}
	base := doc.Run{}
	if len(p.Runs) > 0 {
		base = p.Runs[0]
		base.Image, base.Text = nil, ""
	}
	baseSt := e.styleOf(p, &base)

	if p.Rule {
		h := 12.0
		r := &row{h: h, before: before, after: after, pageBreak: p.PageBreakBefore}
		r.items = append(r.items, &Line{X1: left, Y1: h / 2, X2: width - right, Y2: h / 2, Width: 0.75, Color: ruleColor})
		return []*row{r}
	}

	avail := width - left - right
	pieces := e.pieces(p, math.Max(avail, 36))

	// The list marker hangs in front of the first line.
	var marker []piece
	textStart := left + first // where the first line's text starts
	if p.List != nil {
		mst := baseSt
		mst.underline, mst.strike, mst.highlight, mst.link = false, false, nil, ""
		marker = e.measure(doc.Marker(p.List, number), mst, false)
		mw := 0.0
		for _, m := range marker {
			mw += m.w
		}
		textStart = math.Max(left, left+first+mw+mst.size*0.3)
	}

	// Break into lines.
	var lines []lineOut
	cur := lineOut{start: textStart, avail: width - right - textStart}
	x := 0.0
	trailing := func(l *lineOut) {
		w := 0.0
		for i, pc := range l.pieces {
			if !pc.space && !pc.brk {
				w = l.xs[i] + pc.w
			}
		}
		l.width = w
	}
	endLine := func(last bool) {
		trailing(&cur)
		cur.last = last
		lines = append(lines, cur)
		cur = lineOut{start: left, avail: width - right - left}
		x = 0
	}
	for i := 0; i < len(pieces); i++ {
		pc := pieces[i]
		if pc.brk {
			cur.pieces = append(cur.pieces, pc)
			cur.xs = append(cur.xs, x)
			endLine(true)
			continue
		}
		if pc.tab {
			// To the next tab stop, measured from the column's left edge.
			abs := cur.start + x
			next := (math.Floor(abs/tabStop+1e-6) + 1) * tabStop
			pc.w = next - abs
			if cur.start+x+pc.w > width-right && len(cur.pieces) > 0 {
				endLine(false)
				continue
			}
		}
		// A word with the pieces glued to it (font fallback) breaks as one.
		wordW := pc.w
		j := i + 1
		for j < len(pieces) && pieces[j].glue && !pieces[j].space && !pieces[j].tab && !pieces[j].brk {
			wordW += pieces[j].w
			j++
		}
		if !pc.space && x+wordW > cur.avail+0.01 && len(cur.pieces) > 0 && hasContent(cur.pieces) {
			endLine(false)
		}
		if !pc.space && !pc.tab && pc.img == nil && pc.w > cur.avail+0.01 && x == 0 {
			// A word longer than the line: break it between characters.
			parts := splitPiece(pc, cur.avail)
			for k, part := range parts {
				cur.pieces = append(cur.pieces, part)
				cur.xs = append(cur.xs, x)
				x += part.w
				if k < len(parts)-1 {
					endLine(false)
				}
			}
			continue
		}
		if pc.space && len(cur.pieces) == 0 && len(lines) > 0 && !lines[len(lines)-1].last {
			continue // no spaces at the start of a wrapped line
		}
		cur.pieces = append(cur.pieces, pc)
		cur.xs = append(cur.xs, x)
		x += pc.w
	}
	if len(cur.pieces) > 0 || len(lines) == 0 || lines[len(lines)-1].last && len(pieces) > 0 && pieces[len(pieces)-1].brk {
		endLine(true)
	}
	if n := len(lines); n > 0 {
		lines[n-1].last = true
	}

	// Lines to rows.
	var rows []*row
	for li, ln := range lines {
		asc, desc := 0.0, 0.0
		for _, pc := range ln.pieces {
			asc = math.Max(asc, pc.ascent())
			desc = math.Max(desc, pc.descent())
		}
		if li == 0 {
			for _, m := range marker {
				asc = math.Max(asc, m.ascent())
				desc = math.Max(desc, m.descent())
			}
		}
		if asc == 0 && desc == 0 {
			bp := piece{st: baseSt}
			asc, desc = bp.ascent(), bp.descent()
		}
		natural := asc + desc
		h := natural * spacing
		r := &row{h: h}
		if li == 0 {
			r.before, r.pageBreak = before, p.PageBreakBefore
		}
		if li == len(lines)-1 {
			r.after = after
		}
		r.keepNext = p.Heading > 0
		baseline := asc
		if p.Code {
			r.items = append(r.items, &Rect{X: left, Y: 0, W: width - right - left, H: h, Fill: codeBack})
		}
		// Alignment.
		offset := 0.0
		extra := 0.0
		free := ln.avail - ln.width
		switch p.Align {
		case doc.AlignCenter:
			offset = free / 2
		case doc.AlignRight:
			offset = free
		case doc.AlignJustify:
			if !ln.last && free > 0 {
				spaces := 0
				for k, pc := range ln.pieces {
					if pc.space && ln.xs[k] < ln.width {
						spaces++
					}
				}
				if spaces > 0 {
					extra = free / float64(spaces)
				}
			}
		}
		if offset < 0 {
			offset = 0
		}
		if li == 0 && len(marker) > 0 {
			mx := left + first + offset
			for _, m := range marker {
				e.emit(r, &m, mx, baseline)
				mx += m.w
			}
		}
		shift := 0.0
		for k := range ln.pieces {
			pc := ln.pieces[k]
			px := ln.start + offset + ln.xs[k] + shift
			if pc.space && extra > 0 && ln.xs[k] < ln.width {
				pc.w += extra
				if len(pc.advances) > 0 {
					pc.advances = append([]float64{}, pc.advances...)
					pc.advances[len(pc.advances)-1] += extra
				}
				shift += extra
			}
			if pc.brk || ln.xs[k] >= ln.width && pc.space {
				continue
			}
			if pc.tab {
				continue
			}
			e.emit(r, &pc, px, baseline)
		}
		rows = append(rows, mergeTexts(r))
	}
	return rows
}

func hasContent(ps []piece) bool {
	for _, p := range ps {
		if !p.space {
			return true
		}
	}
	return false
}

// splitPiece breaks a piece into parts that fit a width.
func splitPiece(pc piece, width float64) []piece {
	var parts []piece
	cur := piece{st: pc.st, glue: pc.glue}
	i := 0
	for _, r := range pc.text {
		a := pc.advances[i]
		i++
		if cur.w+a > width && cur.text != "" {
			parts = append(parts, cur)
			cur = piece{st: pc.st}
		}
		cur.text += string(r)
		cur.advances = append(cur.advances, a)
		cur.w += a
	}
	if cur.text != "" {
		parts = append(parts, cur)
	}
	return parts
}

// emit adds the items of a piece at x with the line's baseline.
func (e *engine) emit(r *row, pc *piece, x, baseline float64) {
	if pc.img != nil {
		r.items = append(r.items, &Picture{X: x, Y: baseline - pc.imgH, W: pc.imgW, H: pc.imgH, Image: pc.img})
		if pc.st.link != "" {
			r.items = append(r.items, &Link{X: x, Y: baseline - pc.imgH, W: pc.imgW, H: pc.imgH, URL: pc.st.link})
		}
		return
	}
	if pc.text == "" {
		return
	}
	st := pc.st
	y := baseline - st.shift
	asc, desc := st.size*0.9, st.size*0.25
	if f := st.face.Font; f != nil {
		asc, desc = f.AscentEm()*st.size, f.DescentEm()*st.size
	}
	if st.highlight != nil {
		r.items = append(r.items, &Rect{X: x, Y: y - asc, W: pc.w, H: asc + desc, Fill: *st.highlight})
	}
	r.items = append(r.items, &Text{X: x, Y: y, Text: pc.text, Face: st.face, Size: st.size, Color: st.color,
		Advances: append([]float64{}, pc.advances...)})
	thick := st.size / 18
	pos := st.size * 0.12
	if f := st.face.Font; f != nil {
		thick = math.Max(0.4, float64(f.UnderlineThickness)/float64(f.UnitsPerEm)*st.size)
		pos = -float64(f.UnderlinePos) / float64(f.UnitsPerEm) * st.size
	}
	if st.underline && !pc.space {
		r.items = append(r.items, &Line{X1: x, Y1: y + pos, X2: x + pc.w, Y2: y + pos, Width: thick, Color: st.color})
	}
	if st.strike && !pc.space {
		sy := y - st.size*0.28
		r.items = append(r.items, &Line{X1: x, Y1: sy, X2: x + pc.w, Y2: sy, Width: thick, Color: st.color})
	}
	if st.link != "" {
		r.items = append(r.items, &Link{X: x, Y: y - asc, W: pc.w, H: asc + desc, URL: st.link})
	}
}

// mergeTexts joins neighbouring text items of the same style on a line,
// and the underlines between them, so that a line is drawn in few calls.
func mergeTexts(r *row) *row {
	var out []Item
	for _, it := range r.items {
		t, ok := it.(*Text)
		if ok && len(out) > 0 {
			if prev, ok := out[len(out)-1].(*Text); ok && prev.Face == t.Face && prev.Size == t.Size &&
				prev.Color == t.Color && prev.Y == t.Y && math.Abs(prev.X+prev.Width()-t.X) < 0.01 {
				prev.Text += t.Text
				prev.Advances = append(prev.Advances, t.Advances...)
				continue
			}
		}
		out = append(out, it)
	}
	// Join underlines that touch.
	var merged []Item
	for _, it := range out {
		l, ok := it.(*Line)
		if ok && len(merged) > 0 {
			if prev, ok := merged[len(merged)-1].(*Line); ok && prev.Y1 == l.Y1 && prev.Y2 == l.Y2 && prev.Color == l.Color &&
				prev.Width == l.Width && math.Abs(prev.X2-l.X1) < 0.01 {
				prev.X2 = l.X2
				continue
			}
		}
		merged = append(merged, it)
	}
	r.items = merged
	return r
}

// table lays out a table as rows.
func (e *engine) table(t *doc.Table, width float64) []*row {
	cols := t.Columns()
	if cols == 0 {
		return nil
	}
	widths := e.columnWidths(t, cols, width)
	total := 0.0
	for _, w := range widths {
		total += w
	}
	var rows []*row
	var header *row
	for ri, tr := range t.Rows {
		r := &row{}
		type laid struct {
			x, w float64
			rows []*row
			h    float64
		}
		var cells []laid
		x, col := 0.0, 0
		for _, c := range tr.Cells {
			span := max(1, c.Span)
			w := 0.0
			for s := 0; s < span && col < cols; s++ {
				w += widths[col]
				col++
			}
			blocks := c.Blocks
			if ri == 0 && t.Header {
				blocks = boldBlocks(blocks)
			}
			if len(t.Aligns) > len(cells) {
				blocks = alignBlocks(blocks, t.Aligns[len(cells)])
			}
			inner := e.blocks(blocks, math.Max(1, w-2*cellPadX))
			h := 0.0
			for k, ir := range inner {
				if k > 0 {
					h += ir.before
				}
				h += ir.h
				if k < len(inner)-1 {
					h += ir.after
				}
			}
			cells = append(cells, laid{x: x, w: w, rows: inner, h: h})
			x += w
		}
		rh := 0.0
		for _, c := range cells {
			rh = math.Max(rh, c.h)
		}
		rh += 2 * cellPadY
		r.h = rh
		for _, c := range cells {
			y := cellPadY
			for k, ir := range c.rows {
				if k > 0 {
					y += ir.before
				}
				for _, it := range ir.items {
					it.move(c.x+cellPadX, y)
					r.items = append(r.items, it)
				}
				y += ir.h + ir.after
			}
		}
		// Borders: the top and left of every cell, the right and bottom of
		// the table.
		bw := 0.5
		r.items = append(r.items, &Line{X1: 0, Y1: 0, X2: total, Y2: 0, Width: bw, Color: borderCol})
		r.items = append(r.items, &Line{X1: 0, Y1: rh, X2: total, Y2: rh, Width: bw, Color: borderCol})
		for _, c := range cells {
			r.items = append(r.items, &Line{X1: c.x, Y1: 0, X2: c.x, Y2: rh, Width: bw, Color: borderCol})
		}
		r.items = append(r.items, &Line{X1: total, Y1: 0, X2: total, Y2: rh, Width: bw, Color: borderCol})
		if ri == 0 {
			r.before = 3
			if t.Header {
				header = r
			}
		}
		if ri == len(t.Rows)-1 {
			r.after = 6
		}
		r.header = header
		rows = append(rows, r)
	}
	if header != nil {
		header = cloneRow(header) // a copy, before the rows are placed and moved
		for _, r := range rows {
			r.header = header
		}
		rows[0].header = nil
	}
	return rows
}

func boldBlocks(blocks []doc.Block) []doc.Block {
	out := make([]doc.Block, len(blocks))
	for i, b := range blocks {
		if p, ok := b.(*doc.Paragraph); ok {
			q := *p
			q.Runs = make([]doc.Run, len(p.Runs))
			for k, r := range p.Runs {
				r.Bold = true
				q.Runs[k] = r
			}
			out[i] = &q
			continue
		}
		out[i] = b
	}
	return out
}

func alignBlocks(blocks []doc.Block, a doc.Align) []doc.Block {
	out := make([]doc.Block, len(blocks))
	for i, b := range blocks {
		if p, ok := b.(*doc.Paragraph); ok && p.Align == doc.AlignLeft {
			q := *p
			q.Align = a
			out[i] = &q
			continue
		}
		out[i] = b
	}
	return out
}

// columnWidths shares the width out over the columns: the widths of the
// table, scaled down to fit, or else by the length of the cell text.
func (e *engine) columnWidths(t *doc.Table, cols int, width float64) []float64 {
	widths := make([]float64, cols)
	given := 0.0
	ok := len(t.ColWidths) >= cols
	for i := 0; i < cols && ok; i++ {
		if t.ColWidths[i] <= 0 {
			ok = false
			break
		}
		widths[i] = t.ColWidths[i]
		given += t.ColWidths[i]
	}
	if ok {
		if given > width {
			for i := range widths {
				widths[i] *= width / given
			}
		}
		return widths
	}
	// By content: the longest word is the minimum, the text on one line
	// the most a column wants.
	minW := make([]float64, cols)
	maxW := make([]float64, cols)
	for ri, r := range t.Rows {
		col := 0
		for _, c := range r.Cells {
			span := max(1, c.Span)
			if span == 1 && col < cols {
				lo, hi := e.cellText(c, ri == 0 && t.Header)
				minW[col] = math.Max(minW[col], lo+2*cellPadX)
				maxW[col] = math.Max(maxW[col], hi+2*cellPadX)
			}
			col += span
		}
	}
	sumMax, sumMin := 0.0, 0.0
	for i := range widths {
		maxW[i] = math.Max(maxW[i], 24)
		minW[i] = math.Min(math.Max(minW[i], 24), maxW[i])
		sumMax += maxW[i]
		sumMin += minW[i]
	}
	switch {
	case sumMax <= width:
		copy(widths, maxW)
	case sumMin >= width:
		for i := range widths {
			widths[i] = minW[i] * width / sumMin
		}
	default:
		rest := width - sumMin
		for i := range widths {
			widths[i] = minW[i] + rest*(maxW[i]-minW[i])/(sumMax-sumMin)
		}
	}
	return widths
}

// cellText measures the longest word and the whole text of a cell.
func (e *engine) cellText(c doc.Cell, bold bool) (float64, float64) {
	lo, hi := 0.0, 0.0
	for _, b := range c.Blocks {
		p, ok := b.(*doc.Paragraph)
		if !ok {
			lo, hi = math.Max(lo, 100), math.Max(hi, 200)
			continue
		}
		line, word := 0.0, 0.0
		for i := range p.Runs {
			r := p.Runs[i]
			r.Bold = r.Bold || bold
			st := e.styleOf(p, &r)
			if r.Image != nil {
				lo = math.Max(lo, math.Min(r.Image.Width, 200))
				line += r.Image.Width
				continue
			}
			for _, ch := range r.Text {
				a := st.size * 0.5
				if st.face.Font != nil {
					a = st.face.Font.Advance(ch) * st.size
				}
				if ch == '\n' {
					hi = math.Max(hi, line)
					line, word = 0, 0
					continue
				}
				line += a
				if ch == ' ' {
					word = 0
				} else {
					word += a
					lo = math.Max(lo, word)
				}
			}
		}
		hi = math.Max(hi, line)
	}
	return lo, hi
}

// PageCount lays out a document and counts its pages.
func PageCount(d *doc.Document, s Settings) int { return len(Layout(d, s)) }
