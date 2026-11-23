package doc

import (
	"bytes"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ---- reading ----

// ReadHTML converts an HTML page to a document: headings, paragraphs,
// lists, quotes, preformatted text, tables, links, pictures (local files
// relative to baseDir and data: URLs) and the common inline formatting,
// including the colours, fonts and sizes of style attributes.
func ReadHTML(src []byte, baseDir string) *Document {
	root, err := xhtml.Parse(bytes.NewReader(src))
	d := &Document{}
	if err != nil {
		return FromPlainText(string(src))
	}
	h := &htmlReader{doc: d, base: baseDir}
	body := findElement(root, atom.Body)
	if body == nil {
		body = root
	}
	h.block(body, htmlCtx{})
	h.flush()
	if len(d.Blocks) == 0 {
		d.Blocks = []Block{&Paragraph{}}
	}
	d.Normalize()
	return d
}

func findElement(n *xhtml.Node, a atom.Atom) *xhtml.Node {
	if n.Type == xhtml.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findElement(c, a); f != nil {
			return f
		}
	}
	return nil
}

type htmlCtx struct {
	out     *[]Block // nil: the document
	quote   bool
	pre     bool
	align   Align
	heading int
	list    *List   // the list of the item being read
	indent  float64 // margin-left of the paragraphs, in points
	inItem  bool
	level   int // list nesting, when inList
	inList  bool
	run     Run // the inline style
}

type htmlReader struct {
	doc       *Document
	base      string
	cur       *Paragraph
	curOut    *[]Block
	first     bool // the next paragraph is the first of a list item
	itemList  *List
	itemStart int
}

func (h *htmlReader) add(out *[]Block, b Block) {
	if out != nil {
		*out = append(*out, b)
		return
	}
	h.doc.Blocks = append(h.doc.Blocks, b)
}

// para returns the paragraph that inline content goes into, starting one
// when needed.
func (h *htmlReader) para(ctx htmlCtx) *Paragraph {
	if h.cur != nil {
		return h.cur
	}
	p := &Paragraph{Align: ctx.align, Quote: ctx.quote, Code: ctx.pre, Heading: ctx.heading, LeftIndent: ctx.indent}
	if !ctx.pre && ctx.heading == 0 && !ctx.inList {
		p.SpaceAfter = paraSpace
	}
	if ctx.inList {
		if h.first && ctx.list != nil {
			l := *ctx.list
			l.Level = ctx.level
			if h.itemStart > 0 {
				l.Start = h.itemStart
			}
			p.List = &l
			ctx.list.Start = 0 // only the first item starts the numbers
			h.first, h.itemStart = false, 0
		} else {
			p.LeftIndent = listStep * float64(ctx.level+1)
		}
	}
	h.cur, h.curOut = p, ctx.out
	return p
}

// flush ends the current paragraph.
func (h *htmlReader) flush() {
	p := h.cur
	if p == nil {
		return
	}
	h.cur = nil
	// Trim the white space at the edges of a paragraph.
	if !p.Code {
		for len(p.Runs) > 0 && p.Runs[0].Image == nil && strings.TrimLeft(p.Runs[0].Text, " ") == "" {
			p.Runs = p.Runs[1:]
		}
		if len(p.Runs) > 0 && p.Runs[0].Image == nil {
			p.Runs[0].Text = strings.TrimLeft(p.Runs[0].Text, " ")
		}
		for len(p.Runs) > 0 {
			last := &p.Runs[len(p.Runs)-1]
			if last.Image != nil {
				break
			}
			t := strings.TrimRight(last.Text, " ")
			if t != "" {
				last.Text = t
				break
			}
			p.Runs = p.Runs[:len(p.Runs)-1]
		}
	}
	if len(p.Runs) == 0 && p.List == nil && !p.Code {
		return
	}
	h.add(h.curOut, p)
}

func attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

// styleProps reads a style attribute.
func styleProps(s string) map[string]string {
	m := map[string]string{}
	for _, decl := range strings.Split(s, ";") {
		kv := strings.SplitN(decl, ":", 2)
		if len(kv) == 2 {
			m[strings.ToLower(strings.TrimSpace(kv[0]))] = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(kv[1]), "!important"))
		}
	}
	return m
}

var namedColors = map[string]Color{
	"black": {0, 0, 0}, "white": {255, 255, 255}, "red": {255, 0, 0}, "green": {0, 128, 0}, "blue": {0, 0, 255},
	"yellow": {255, 255, 0}, "gray": {128, 128, 128}, "grey": {128, 128, 128}, "silver": {192, 192, 192},
	"maroon": {128, 0, 0}, "purple": {128, 0, 128}, "fuchsia": {255, 0, 255}, "magenta": {255, 0, 255},
	"lime": {0, 255, 0}, "olive": {128, 128, 0}, "navy": {0, 0, 128}, "teal": {0, 128, 128}, "aqua": {0, 255, 255},
	"cyan": {0, 255, 255}, "orange": {255, 165, 0}, "darkred": {139, 0, 0}, "darkblue": {0, 0, 139},
	"darkgreen": {0, 100, 0}, "brown": {165, 42, 42}, "pink": {255, 192, 203},
}

func parseCSSColor(s string) *Color {
	s = strings.ToLower(strings.TrimSpace(s))
	if c, ok := namedColors[s]; ok {
		return &c
	}
	if strings.HasPrefix(s, "#") && len(s) == 4 {
		s = "#" + string([]byte{s[1], s[1], s[2], s[2], s[3], s[3]})
	}
	if c, ok := parseHexColor(s); ok {
		return c
	}
	if strings.HasPrefix(s, "rgb") {
		open, end := strings.IndexByte(s, '('), strings.IndexByte(s, ')')
		if open > 0 && end > open {
			parts := strings.FieldsFunc(s[open+1:end], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
			if len(parts) >= 3 {
				var v [3]uint8
				for i := 0; i < 3; i++ {
					f, err := strconv.ParseFloat(strings.TrimSuffix(parts[i], "%"), 64)
					if err != nil {
						return nil
					}
					if strings.HasSuffix(parts[i], "%") {
						f *= 2.55
					}
					v[i] = uint8(math.Max(0, math.Min(255, f)))
				}
				if len(parts) >= 4 && (parts[3] == "0" || parts[3] == "0%") {
					return nil // transparent
				}
				return &Color{v[0], v[1], v[2]}
			}
		}
	}
	return nil
}

// applyStyle changes a run style with CSS.
func applyCSS(r *Run, css map[string]string) {
	if v, ok := css["font-weight"]; ok {
		n, err := strconv.Atoi(v)
		r.Bold = v == "bold" || v == "bolder" || err == nil && n >= 600
	}
	if v, ok := css["font-style"]; ok {
		r.Italic = v == "italic" || v == "oblique"
	}
	for _, k := range []string{"text-decoration", "text-decoration-line"} {
		if v, ok := css[k]; ok {
			r.Underline = strings.Contains(v, "underline")
			r.Strike = strings.Contains(v, "line-through")
		}
	}
	if v, ok := css["color"]; ok {
		r.Color = parseCSSColor(v)
	}
	for _, k := range []string{"background-color", "background"} {
		if v, ok := css[k]; ok {
			r.Highlight = parseCSSColor(v)
		}
	}
	if v, ok := css["font-family"]; ok {
		fam := strings.Trim(strings.TrimSpace(strings.Split(v, ",")[0]), `'"`)
		if fam == "monospace" {
			fam = CodeFont
		}
		if fam != "" && fam != "inherit" && fam != "sans-serif" && fam != "serif" {
			r.Font = fam
		}
	}
	if v, ok := css["font-size"]; ok {
		if pt, ok := parseLength(v); ok && !strings.HasSuffix(v, "%") && !strings.HasSuffix(v, "em") {
			r.Size = math.Round(pt*2) / 2
		}
	}
	if v, ok := css["vertical-align"]; ok {
		r.Super, r.Sub = v == "super", v == "sub"
	}
}

func alignOf(n *xhtml.Node, css map[string]string, def Align) Align {
	v := strings.ToLower(attr(n, "align"))
	if s, ok := css["text-align"]; ok {
		v = strings.ToLower(s)
	}
	switch v {
	case "center":
		return AlignCenter
	case "right", "end":
		return AlignRight
	case "justify":
		return AlignJustify
	case "left", "start":
		return AlignLeft
	}
	return def
}

// marginLeft is the left indent of a block with a margin-left. In a
// blockquote the margin comes on top of the quote's own indent (see
// WriteHTML).
func marginLeft(css map[string]string, ctx htmlCtx) float64 {
	v, ok := css["margin-left"]
	if !ok {
		return ctx.indent
	}
	m, ok := parseLength(v)
	if !ok || strings.HasSuffix(v, "%") || m <= 0 {
		return ctx.indent
	}
	if ctx.quote && ctx.indent == 0 {
		return listStep/2 + m
	}
	return ctx.indent + m
}

func (h *htmlReader) block(n *xhtml.Node, ctx htmlCtx) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		h.node(c, ctx)
	}
}

func (h *htmlReader) node(n *xhtml.Node, ctx htmlCtx) {
	switch n.Type {
	case xhtml.TextNode:
		h.text(n.Data, ctx)
		return
	case xhtml.ElementNode:
	default:
		return
	}
	css := styleProps(attr(n, "style"))
	if css["display"] == "none" {
		return
	}
	inner := ctx
	applyCSS(&inner.run, css)
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Head, atom.Title, atom.Noscript, atom.Template, atom.Button, atom.Select, atom.Input, atom.Textarea, atom.Svg:
		return
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		h.flush()
		inner.heading = int(n.Data[1] - '0')
		inner.align = alignOf(n, css, ctx.align)
		inner.run.Bold = false
		h.block(n, inner)
		h.flush()
	case atom.P, atom.Div, atom.Section, atom.Article, atom.Header, atom.Footer, atom.Main, atom.Nav, atom.Aside,
		atom.Figure, atom.Figcaption, atom.Address, atom.Dd, atom.Dt, atom.Dl, atom.Center, atom.Form, atom.Fieldset, atom.Details, atom.Summary:
		h.flush()
		inner.align = alignOf(n, css, ctx.align)
		if n.DataAtom == atom.Center {
			inner.align = AlignCenter
		}
		inner.indent = marginLeft(css, ctx)
		h.block(n, inner)
		h.flush()
	case atom.Br:
		p := h.para(ctx)
		r := ctx.run
		r.Text = "\n"
		p.Runs = append(p.Runs, r)
	case atom.Hr:
		h.flush()
		h.add(ctx.out, &Paragraph{Rule: true, SpaceAfter: paraSpace})
	case atom.Blockquote:
		h.flush()
		inner.quote = true
		inner.indent = marginLeft(css, ctx)
		h.block(n, inner)
		h.flush()
	case atom.Pre:
		h.flush()
		inner.pre = true
		// The text of a <pre> keeps its line breaks: one paragraph per line.
		var b strings.Builder
		var collect func(*xhtml.Node)
		collect = func(x *xhtml.Node) {
			if x.Type == xhtml.TextNode {
				b.WriteString(x.Data)
			} else if x.DataAtom == atom.Br {
				b.WriteString("\n")
			}
			for c := x.FirstChild; c != nil; c = c.NextSibling {
				collect(c)
			}
		}
		collect(n)
		text := strings.TrimSuffix(strings.TrimPrefix(b.String(), "\n"), "\n")
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		for i, line := range lines {
			p := &Paragraph{Code: true, Quote: ctx.quote}
			if ctx.inList {
				p.LeftIndent = listStep * float64(ctx.level+1)
			}
			if i == len(lines)-1 {
				p.SpaceAfter = paraSpace
			}
			if line != "" {
				p.Runs = []Run{{Text: line}}
			}
			h.add(ctx.out, p)
		}
	case atom.Ul, atom.Ol, atom.Menu:
		h.flush()
		l := &List{Ordered: n.DataAtom == atom.Ol}
		if l.Ordered {
			l.Start = max(1, atoiDefault(attr(n, "start"), 1))
			switch attr(n, "type") {
			case "a":
				l.Format = LowerAlpha
			case "A":
				l.Format = UpperAlpha
			case "i":
				l.Format = LowerRoman
			case "I":
				l.Format = UpperRoman
			}
			switch css["list-style-type"] {
			case "lower-alpha", "lower-latin":
				l.Format = LowerAlpha
			case "upper-alpha", "upper-latin":
				l.Format = UpperAlpha
			case "lower-roman":
				l.Format = LowerRoman
			case "upper-roman":
				l.Format = UpperRoman
			}
		}
		if ctx.inList {
			inner.level = ctx.level + 1
		} else {
			inner.level = 0
		}
		inner.inList, inner.list = true, l
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && c.DataAtom == atom.Li {
				h.flush()
				h.first = true
				h.itemStart = atoiDefault(attr(c, "value"), 0)
				ic := inner
				applyCSS(&ic.run, styleProps(attr(c, "style")))
				h.block(c, ic)
				if h.first { // an empty item
					h.para(ic)
				}
				h.flush()
			} else {
				h.node(c, inner)
			}
		}
		h.flush()
		if !ctx.inList {
			var blocks []Block
			if ctx.out != nil {
				blocks = *ctx.out
			} else {
				blocks = h.doc.Blocks
			}
			if k := len(blocks); k > 0 {
				if p, ok := blocks[k-1].(*Paragraph); ok && p.SpaceAfter == 0 {
					p.SpaceAfter = paraSpace
				}
			}
		}
	case atom.Table:
		h.flush()
		h.table(n, ctx)
	case atom.Img:
		if img := h.image(attr(n, "src")); img != nil {
			img.Alt = attr(n, "alt")
			if w, ok := parseLength(attr(n, "width")); ok && w > 0 {
				ratio := img.Height / img.Width
				img.Width, img.Height = w*0.75, w*0.75*ratio
			}
			if v, ok := css["width"]; ok {
				if w, ok := parseLength(v); ok && w > 0 && !strings.HasSuffix(v, "%") {
					ratio := img.Height / img.Width
					img.Width, img.Height = w, w*ratio
				}
			}
			p := h.para(ctx)
			p.Runs = append(p.Runs, Run{Image: img, Link: ctx.run.Link})
		} else if alt := attr(n, "alt"); alt != "" {
			h.text(alt, ctx)
		}
	case atom.B, atom.Strong:
		inner.run.Bold = true
		h.block(n, inner)
	case atom.I, atom.Em, atom.Cite, atom.Var, atom.Dfn:
		inner.run.Italic = true
		h.block(n, inner)
	case atom.U, atom.Ins:
		inner.run.Underline = true
		h.block(n, inner)
	case atom.S, atom.Del, atom.Strike:
		inner.run.Strike = true
		h.block(n, inner)
	case atom.Sub:
		inner.run.Sub, inner.run.Super = true, false
		h.block(n, inner)
	case atom.Sup:
		inner.run.Super, inner.run.Sub = true, false
		h.block(n, inner)
	case atom.Code, atom.Kbd, atom.Tt, atom.Samp:
		if !ctx.pre {
			inner.run.Font, inner.run.Size, inner.run.Highlight = CodeFont, CodeSize, &codeBackground
		}
		h.block(n, inner)
	case atom.Mark:
		inner.run.Highlight = &Color{0xFF, 0xFF, 0x00}
		h.block(n, inner)
	case atom.A:
		if href := attr(n, "href"); href != "" && !strings.HasPrefix(strings.ToLower(href), "javascript:") {
			inner.run.Link = href
		}
		h.block(n, inner)
	case atom.Font:
		if c := parseCSSColor(attr(n, "color")); c != nil {
			inner.run.Color = c
		}
		if f := attr(n, "face"); f != "" {
			inner.run.Font = strings.TrimSpace(strings.Split(f, ",")[0])
		}
		if s := atoiDefault(attr(n, "size"), 0); s >= 1 && s <= 7 {
			inner.run.Size = []float64{0, 7.5, 10, 12, 13.5, 18, 24, 36}[s]
		}
		h.block(n, inner)
	default:
		h.block(n, inner)
	}
}

func (h *htmlReader) text(s string, ctx htmlCtx) {
	if !ctx.pre {
		s = collapseSpaces(s)
		if strings.TrimSpace(s) == "" && h.cur == nil {
			return // white space between blocks
		}
	}
	p := h.para(ctx)
	r := ctx.run
	r.Text = s
	// No double spaces across runs.
	if n := len(p.Runs); n > 0 && strings.HasSuffix(p.Runs[n-1].Text, " ") && strings.HasPrefix(r.Text, " ") {
		r.Text = r.Text[1:]
	}
	if r.Text != "" {
		p.Runs = append(p.Runs, r)
	}
}

func (h *htmlReader) table(n *xhtml.Node, ctx htmlCtx) {
	t := &Table{}
	var rows []*xhtml.Node
	var collect func(*xhtml.Node, bool)
	header := false
	collect = func(x *xhtml.Node, inHead bool) {
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xhtml.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Thead:
				collect(c, true)
			case atom.Tbody, atom.Tfoot:
				collect(c, false)
			case atom.Tr:
				if inHead && len(rows) == 0 {
					header = true
				}
				rows = append(rows, c)
			case atom.Table:
				// nested tables are read as text of their cell
			}
		}
	}
	collect(n, false)
	for ri, tr := range rows {
		var row Row
		allTh := true
		for c := tr.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != xhtml.ElementNode || (c.DataAtom != atom.Td && c.DataAtom != atom.Th) {
				continue
			}
			if c.DataAtom == atom.Td {
				allTh = false
			}
			var blocks []Block
			def := AlignLeft
			if c.DataAtom == atom.Th {
				def = AlignCenter // as browsers show a header cell
			}
			cctx := htmlCtx{out: &blocks, run: ctx.run, align: alignOf(c, styleProps(attr(c, "style")), def)}
			h.block(c, cctx)
			h.flush()
			for _, b := range blocks {
				if p, ok := b.(*Paragraph); ok {
					p.SpaceAfter = 0
				}
			}
			if len(blocks) == 0 {
				blocks = []Block{&Paragraph{}}
			}
			row.Cells = append(row.Cells, Cell{Blocks: blocks, Span: atoiDefault(attr(c, "colspan"), 1)})
		}
		if ri == 0 && allTh && len(row.Cells) > 0 {
			header = true
		}
		t.Rows = append(t.Rows, row)
	}
	if len(t.Rows) == 0 {
		return
	}
	t.Header = header
	h.add(ctx.out, t)
}

func (h *htmlReader) image(src string) *Image {
	m := &mdReader{base: h.base}
	return m.image(src)
}

// ---- writing ----

// WriteHTML converts a document to an HTML page. Pictures are stored with
// save (embedded as data: URLs when it is nil).
func WriteHTML(d *Document, title string, save ImageSaver) ([]byte, error) {
	if save == nil {
		save = DataImageSaver
	}
	w := &htmlWriter{save: save}
	w.b.WriteString("<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n")
	w.b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&w.b, "<title>%s</title>\n", html.EscapeString(title))
	w.b.WriteString(`<style>
body { font-family: Calibri, Carlito, "Segoe UI", Arial, sans-serif; font-size: 11pt; line-height: 1.3; max-width: 46em; margin: 2em auto; padding: 0 1em; color: #000; background: #fff; }
p { margin: 0 0 6pt; }
h1, h2, h3, h4, h5, h6 { margin: 12pt 0 3pt; }
h1 { font-size: 24pt; } h2 { font-size: 18pt; } h3 { font-size: 15pt; } h4 { font-size: 13pt; } h5 { font-size: 11.5pt; } h6 { font-size: 10.5pt; }
pre { font-family: Consolas, monospace; font-size: 10pt; background: #f0f0f0; padding: 4pt 6pt; margin: 0 0 6pt; white-space: pre-wrap; }
code { font-family: Consolas, monospace; font-size: 10pt; background: #f0f0f0; }
blockquote { color: #595959; margin: 0 0 6pt 18pt; }
table { border-collapse: collapse; margin: 3pt 0 6pt; }
td, th { border: 0.5pt solid #808080; padding: 3pt 5pt; vertical-align: top; }
th { text-align: left; }
ul, ol { margin: 0 0 6pt; padding-left: 36pt; }
li > ul, li > ol { margin: 0; padding-left: 36pt; }
img { max-width: 100%; }
hr { border: 0; border-top: 0.75pt solid #a0a0a0; margin: 6pt 0; }
</style>
</head>
<body>
`)
	w.blocks(d.Blocks)
	w.b.WriteString("</body>\n</html>\n")
	return w.b.Bytes(), w.err
}

type htmlWriter struct {
	b       bytes.Buffer
	save    ImageSaver
	err     error
	inQuote bool // in a blockquote, which indents by listStep/2
}

func (w *htmlWriter) blocks(blocks []Block) {
	nums := Numbering(blocks)
	for i := 0; i < len(blocks); i++ {
		switch v := blocks[i].(type) {
		case *Table:
			w.table(v)
		case *Paragraph:
			switch {
			case v.List != nil:
				i = w.list(blocks, i, nums) - 1
			case isCodePara(v):
				w.b.WriteString("<pre><code>")
				for ; i < len(blocks); i++ {
					p, ok := blocks[i].(*Paragraph)
					if !ok || !isCodePara(p) || p.List != nil {
						break
					}
					w.b.WriteString(html.EscapeString(p.Text()))
					w.b.WriteString("\n")
				}
				i--
				w.b.WriteString("</code></pre>\n")
			case isQuotePara(v):
				w.b.WriteString("<blockquote>\n")
				w.inQuote = true
				for ; i < len(blocks); i++ {
					p, ok := blocks[i].(*Paragraph)
					if !ok || !isQuotePara(p) || p.List != nil || isCodePara(p) {
						break
					}
					w.paragraph(p, "p")
				}
				w.inQuote = false
				i--
				w.b.WriteString("</blockquote>\n")
			default:
				w.paragraph(v, "")
			}
		}
	}
}

// list writes the list that starts at blocks[i], with its nested lists,
// and returns the index after it.
func (w *htmlWriter) list(blocks []Block, i int, nums map[*Paragraph]int) int {
	first := blocks[i].(*Paragraph)
	level := first.List.Level
	tag := "ul"
	if first.List.Ordered {
		tag = "ol"
	}
	w.b.WriteString("<" + tag)
	if first.List.Ordered {
		if n := nums[first]; n > 1 {
			fmt.Fprintf(&w.b, ` start="%d"`, n)
		}
		if t := []string{"", "a", "A", "i", "I"}[first.List.Format]; t != "" {
			fmt.Fprintf(&w.b, ` type="%s"`, t)
		}
	}
	w.b.WriteString(">\n")
	open := false
	for i < len(blocks) {
		p, ok := blocks[i].(*Paragraph)
		if !ok {
			break
		}
		switch {
		case p.List != nil && p.List.Level == level && p.List.Ordered == first.List.Ordered:
			if open {
				w.b.WriteString("</li>\n")
			}
			w.b.WriteString("<li>")
			w.inline(p.Runs)
			open = true
			i++
		case p.List != nil && p.List.Level > level && open:
			w.b.WriteString("\n")
			i = w.list(blocks, i, nums)
		case p.List == nil && open && p.LeftIndent >= listStep*float64(level+1) && !p.Quote:
			w.paragraph(p, "p") // a further paragraph of the item
			i++
		default:
			goto done
		}
	}
done:
	if open {
		w.b.WriteString("</li>\n")
	}
	w.b.WriteString("</" + tag + ">\n")
	return i
}

func (w *htmlWriter) paragraph(p *Paragraph, force string) {
	tag := "p"
	if force != "" {
		tag = force
	}
	if p.Heading > 0 {
		tag = "h" + strconv.Itoa(p.Heading)
	}
	if p.Rule {
		w.b.WriteString("<hr>\n")
		return
	}
	var style []string
	switch p.Align {
	case AlignCenter:
		style = append(style, "text-align: center")
	case AlignRight:
		style = append(style, "text-align: right")
	case AlignJustify:
		style = append(style, "text-align: justify")
	}
	indent := p.LeftIndent
	if w.inQuote {
		indent -= listStep / 2 // what the blockquote does not indent
	}
	if indent >= 0.5 && p.List == nil {
		style = append(style, fmt.Sprintf("margin-left: %gpt", round1(indent)))
	}
	if p.FirstIndent != 0 && p.List == nil {
		style = append(style, fmt.Sprintf("text-indent: %gpt", round1(p.FirstIndent)))
	}
	if p.LineSpacing > 0 && p.LineSpacing != 1 {
		style = append(style, fmt.Sprintf("line-height: %g", round1(p.LineSpacing*1.15)))
	}
	if p.PageBreakBefore {
		style = append(style, "break-before: page")
	}
	w.b.WriteString("<" + tag)
	if len(style) > 0 {
		w.b.WriteString(` style="` + strings.Join(style, "; ") + `"`)
	}
	w.b.WriteString(">")
	runs := p.Runs
	if p.Heading > 0 {
		runs = stripBold(runs)
		for i := range runs {
			if runs[i].Size == HeadingSize(p.Heading) {
				runs[i].Size = 0
			}
		}
	}
	if len(runs) == 0 {
		w.b.WriteString("<br>")
	}
	w.inline(runs)
	w.b.WriteString("</" + tag + ">\n")
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func colorCSS(c Color) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

func (w *htmlWriter) inline(runs []Run) {
	for i := 0; i < len(runs); i++ {
		r := runs[i]
		if r.Image != nil {
			url, err := w.save(r.Image)
			if err != nil {
				if w.err == nil {
					w.err = err
				}
				continue
			}
			img := fmt.Sprintf(`<img src="%s" alt="%s" style="width: %gpt; height: %gpt">`,
				html.EscapeString(url), html.EscapeString(r.Image.Alt), round1(r.Image.Width), round1(r.Image.Height))
			if r.Link != "" {
				img = `<a href="` + html.EscapeString(r.Link) + `">` + img + `</a>`
			}
			w.b.WriteString(img)
			continue
		}
		text := html.EscapeString(r.Text)
		text = strings.ReplaceAll(text, "\n", "<br>")
		text = strings.ReplaceAll(text, "\t", "&emsp;")
		if strings.Contains(text, "  ") {
			text = strings.ReplaceAll(text, "  ", " &nbsp;")
		}
		var css []string
		mono := IsMonospace(r.Font)
		if r.Font != "" && !strings.EqualFold(r.Font, DefaultFont) && !mono {
			css = append(css, fmt.Sprintf("font-family: '%s'", strings.ReplaceAll(r.Font, "'", "")))
		}
		if r.Size > 0 && r.Size != DefaultSize && !(mono && r.Size == CodeSize) {
			css = append(css, fmt.Sprintf("font-size: %gpt", r.Size))
		}
		if r.Color != nil && *r.Color != quoteColor && *r.Color != ruleColor {
			css = append(css, "color: "+colorCSS(*r.Color))
		}
		if r.Highlight != nil && !(mono && *r.Highlight == codeBackground) {
			css = append(css, "background-color: "+colorCSS(*r.Highlight))
		}
		if mono {
			text = "<code>" + text + "</code>"
		}
		if len(css) > 0 {
			text = `<span style="` + strings.Join(css, "; ") + `">` + text + `</span>`
		}
		wrap := func(tag string) { text = "<" + tag + ">" + text + "</" + tag + ">" }
		if r.Strike {
			wrap("s")
		}
		if r.Underline && r.Link == "" {
			wrap("u")
		}
		if r.Italic {
			wrap("em")
		}
		if r.Bold {
			wrap("strong")
		}
		if r.Super {
			wrap("sup")
		} else if r.Sub {
			wrap("sub")
		}
		if r.Link != "" {
			text = `<a href="` + html.EscapeString(r.Link) + `">` + text + `</a>`
		}
		w.b.WriteString(text)
	}
}

func (w *htmlWriter) table(t *Table) {
	if len(t.Rows) == 0 {
		return
	}
	w.b.WriteString("<table>\n")
	for ri, row := range t.Rows {
		header := ri == 0 && t.Header
		w.b.WriteString("<tr>")
		for ci, c := range row.Cells {
			tag := "td"
			if header {
				tag = "th"
			}
			w.b.WriteString("<" + tag)
			if c.Span > 1 {
				fmt.Fprintf(&w.b, ` colspan="%d"`, c.Span)
			}
			align := AlignLeft
			if ci < len(t.Aligns) {
				align = t.Aligns[ci]
			}
			if len(c.Blocks) > 0 {
				if p, ok := c.Blocks[0].(*Paragraph); ok && p.Align != AlignLeft {
					align = p.Align
				}
			}
			switch {
			case align == AlignCenter:
				w.b.WriteString(` style="text-align: center"`)
			case align == AlignRight:
				w.b.WriteString(` style="text-align: right"`)
			case header: // a header cell is centred by default
				w.b.WriteString(` style="text-align: left"`)
			}
			w.b.WriteString(">")
			prevPara := false
			for _, blk := range c.Blocks {
				switch v := blk.(type) {
				case *Paragraph:
					if prevPara {
						w.b.WriteString("<br>")
					}
					runs := v.Runs
					if header {
						runs = stripBold(runs)
					}
					w.inline(runs)
					prevPara = true
				case *Table:
					w.b.WriteString("\n")
					w.table(v)
					prevPara = false
				}
			}
			w.b.WriteString("</" + tag + ">")
		}
		w.b.WriteString("</tr>\n")
	}
	w.b.WriteString("</table>\n")
}
