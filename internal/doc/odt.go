package doc

import (
	"fmt"
	"strconv"
	"strings"
)

// ReadODT reads an OpenDocument text file (LibreOffice Writer): paragraphs
// with their styles, headings, lists, tables, links and pictures. Notes,
// headers, footers and frames other than pictures are left out.
func ReadODT(data []byte) (*Document, error) {
	z, err := openZip(data)
	if err != nil {
		return nil, fmt.Errorf("not an OpenDocument file: %w", err)
	}
	content := z.xml("content.xml")
	text := content.find("body").child("text")
	if text == nil {
		return nil, fmt.Errorf("not an OpenDocument text: no office:text in content.xml")
	}
	r := &odtReader{z: z, styles: map[string]*odtStyle{}, listStyles: map[string]map[int]odtListLevel{}, fonts: map[string]string{}}
	styles := z.xml("styles.xml")
	for _, root := range []*node{styles, content} {
		if root == nil {
			continue
		}
		for _, decls := range root.all("font-face-decls") {
			for _, f := range decls.all("font-face") {
				fam := strings.Trim(f.attr("font-family"), "'\"")
				if fam == "" {
					fam = f.attr("name")
				}
				r.fonts[f.attr("name")] = fam
			}
		}
		for _, sec := range []string{"styles", "automatic-styles"} {
			r.readStyles(root.child(sec))
		}
	}
	d := &Document{Blocks: r.blocks(text, odtCtx{})}
	if len(d.Blocks) == 0 {
		d.Blocks = []Block{&Paragraph{}}
	}
	d.Normalize()
	return d, nil
}

type odtStyle struct {
	family, parent, display string
	r                       rProps
	p                       pProps
	outline                 int
	listStyle               string
	colWidth                float64
	resolving               bool
}

type odtListLevel struct {
	bullet bool
	format NumberFormat
	start  int
}

type odtReader struct {
	z           zipFiles
	styles      map[string]*odtStyle
	defaultPara *odtStyle
	listStyles  map[string]map[int]odtListLevel
	fonts       map[string]string
	lists       map[string]*odtCounter // by xml:id, for text:continue-list
	lastList    *odtCounter
}

// odtCounter numbers the items of a list and of the lists that continue it.
type odtCounter struct {
	n []int // the last number per level
}

func (r *odtReader) readStyles(sec *node) {
	if sec == nil {
		return
	}
	for _, s := range sec.children {
		switch s.name {
		case "style", "default-style":
			st := &odtStyle{family: s.attr("family"), parent: s.attr("parent-style-name"), display: s.attr("display-name")}
			if tp := s.child("text-properties"); tp != nil {
				st.r = r.textProps(tp)
			}
			if pp := s.child("paragraph-properties"); pp != nil {
				st.p = r.paraProps(pp)
			}
			if cp := s.child("table-column-properties"); cp != nil {
				st.colWidth, _ = parseLength(cp.attr("column-width"))
			}
			st.outline = atoiDefault(s.attr("default-outline-level"), 0)
			st.listStyle = s.attr("list-style-name")
			if s.name == "default-style" {
				if st.family == "paragraph" {
					r.defaultPara = st
				}
				continue
			}
			r.styles[s.attr("name")] = st
		case "list-style":
			levels := map[int]odtListLevel{}
			for _, l := range s.children {
				lvl := atoiDefault(l.attr("level"), 1) - 1
				switch l.name {
				case "list-level-style-bullet":
					levels[lvl] = odtListLevel{bullet: true}
				case "list-level-style-number":
					ll := odtListLevel{start: atoiDefault(l.attr("start-value"), 1)}
					switch l.attr("num-format") {
					case "a":
						ll.format = LowerAlpha
					case "A":
						ll.format = UpperAlpha
					case "i":
						ll.format = LowerRoman
					case "I":
						ll.format = UpperRoman
					case "":
						ll.bullet = true // no numbers
					}
					levels[lvl] = ll
				}
			}
			r.listStyles[s.attr("name")] = levels
		}
	}
}

func (r *odtReader) textProps(n *node) rProps {
	var p rProps
	switch n.attr("font-weight") {
	case "bold", "600", "700", "800", "900":
		p.bold = yes
	case "normal", "400":
		p.bold = no
	}
	switch n.attr("font-style") {
	case "italic", "oblique":
		p.italic = yes
	case "normal":
		p.italic = no
	}
	if u := n.attr("text-underline-style"); u != "" {
		p.underline = yes
		if u == "none" {
			p.underline = no
		}
	}
	if s := n.attr("text-line-through-style"); s != "" {
		p.strike = yes
		if s == "none" {
			p.strike = no
		}
	}
	if pos := n.attr("text-position"); pos != "" {
		// "super", "sub" or the raising in percent, then the size.
		v := 0.0
		if f := strings.Fields(pos); len(f) > 0 {
			switch f[0] {
			case "super":
				v = 1
			case "sub":
				v = -1
			default:
				v, _ = strconv.ParseFloat(strings.TrimSuffix(f[0], "%"), 64)
			}
		}
		switch {
		case v > 0:
			p.super, p.sub = yes, no
		case v < 0:
			p.sub, p.super = yes, no
		default:
			p.super, p.sub = no, no
		}
	}
	if sz := n.attr("font-size"); sz != "" && !strings.HasSuffix(sz, "%") {
		if v, ok := parseLength(sz); ok && v > 0 {
			p.size = v
		}
	}
	if f := n.attr("font-name"); f != "" {
		if fam, ok := r.fonts[f]; ok {
			p.font = fam
		} else {
			p.font = f
		}
	} else if f := n.attr("font-family"); f != "" {
		p.font = strings.Trim(f, "'\"")
	}
	if c := n.attr("color"); c != "" {
		if col, ok := parseHexColor(c); ok {
			p.color = col
		}
	}
	if n.attr("use-window-font-color") == "true" {
		p.autoColor, p.color = true, nil
	}
	if bg := n.attr("background-color"); bg != "" && bg != "transparent" {
		if col, ok := parseHexColor(bg); ok {
			p.highlight = col
		}
	}
	if n.attr("display") == "none" {
		p.hidden = yes
	}
	return p
}

func lengthPtr(s string) *float64 {
	if s == "" {
		return nil
	}
	if v, ok := parseLength(s); ok {
		return &v
	}
	return nil
}

func (r *odtReader) paraProps(n *node) pProps {
	var p pProps
	if a := n.attr("text-align"); a != "" {
		var al Align
		switch a {
		case "center":
			al = AlignCenter
		case "end", "right":
			al = AlignRight
		case "justify":
			al = AlignJustify
		}
		p.align = &al
	}
	p.left = lengthPtr(n.attr("margin-left"))
	p.right = lengthPtr(n.attr("margin-right"))
	p.first = lengthPtr(n.attr("text-indent"))
	p.before = lengthPtr(n.attr("margin-top"))
	p.after = lengthPtr(n.attr("margin-bottom"))
	if lh := n.attr("line-height"); strings.HasSuffix(lh, "%") {
		if v, err := strconv.ParseFloat(strings.TrimSuffix(lh, "%"), 64); err == nil {
			m := v / 100
			p.line = &m
		}
	} else if lh != "" && lh != "normal" || n.attr("line-height-at-least") != "" {
		zero := 0.0 // an exact or minimal height: no multiple
		p.line = &zero
	}
	p.pageBreak = n.attr("break-before") == "page"
	if b := n.attr("border-bottom"); b != "" && b != "none" {
		p.borderBottom = true
	}
	return p
}

// style resolves a style with its parents and the default style.
func (r *odtReader) style(name string) (rProps, pProps, *odtStyle) {
	st := r.styles[name]
	if st == nil || st.resolving {
		if r.defaultPara != nil {
			return r.defaultPara.r, r.defaultPara.p, nil
		}
		return rProps{}, pProps{}, nil
	}
	st.resolving = true
	defer func() { st.resolving = false }()
	br, bp, _ := r.style(st.parent)
	return st.r.over(br), st.p.over(bp), st
}

// styleChain tells whether a style or one of its parents has a name (or
// display name) for which match is true, such as "Heading 1".
func (r *odtReader) styleChain(name string, match func(string) bool) bool {
	for i := 0; i < 20 && name != ""; i++ {
		st := r.styles[name]
		disp := strings.ReplaceAll(name, "_20_", " ")
		if st != nil && st.display != "" {
			disp = st.display
		}
		if match(strings.ToLower(disp)) {
			return true
		}
		if st == nil {
			return false
		}
		name = st.parent
	}
	return false
}

// listStyleOf finds the list style of a paragraph style, through parents.
func (r *odtReader) listStyleOf(name string) string {
	for i := 0; i < 20 && name != ""; i++ {
		st := r.styles[name]
		if st == nil {
			return ""
		}
		if st.listStyle != "" {
			return st.listStyle
		}
		name = st.parent
	}
	return ""
}

type odtCtx struct {
	listStyle string
	level     int
	inList    bool
	firstItem bool // the next paragraph is the first of a list item
	// The next item is the first of its list, which starts the numbers
	// again unless the list continues the one before.
	firstOfList bool
	startValue  int // text:start-value of the list item
	header      bool
	counter     *odtCounter
}

func (r *odtReader) blocks(parent *node, ctx odtCtx) []Block {
	var out []Block
	for _, c := range parent.children {
		switch c.name {
		case "p", "h":
			p := r.paragraph(c, &ctx)
			out = append(out, p)
		case "list":
			inner := ctx
			if s := c.attr("style-name"); s != "" {
				inner.listStyle = s
			}
			if ctx.inList {
				inner.level = ctx.level + 1
			} else {
				inner.level = 0
				// A list continues the numbers of the list it names, or
				// of the one before it.
				cnt := &odtCounter{}
				if id := c.attr("continue-list"); id != "" && r.lists[id] != nil {
					cnt = r.lists[id]
				} else if c.attr("continue-numbering") == "true" && r.lastList != nil {
					cnt = r.lastList
				}
				if id := c.attr("id"); id != "" {
					if r.lists == nil {
						r.lists = map[string]*odtCounter{}
					}
					r.lists[id] = cnt
				}
				r.lastList = cnt
				inner.counter = cnt
			}
			inner.inList = true
			first := c.attr("continue-numbering") != "true" && c.attr("continue-list") == ""
			for _, item := range c.children {
				if item.name != "list-item" && item.name != "list-header" {
					continue
				}
				ic := inner
				ic.firstItem = item.name == "list-item"
				ic.header = item.name == "list-header"
				ic.firstOfList = first && ic.firstItem
				ic.startValue = atoiDefault(item.attr("start-value"), 0)
				out = append(out, r.blocks(item, ic)...)
				if ic.firstItem {
					first = false
				}
			}
		case "table":
			if t := r.table(c); t != nil {
				out = append(out, t)
			}
		case "section", "index-body", "table-of-content", "alphabetical-index", "illustration-index",
			"bibliography", "user-index", "change-start", "soft-page-break":
			if c.name == "table-of-content" || c.name == "alphabetical-index" || c.name == "illustration-index" ||
				c.name == "bibliography" || c.name == "user-index" {
				out = append(out, r.blocks(c.child("index-body"), ctx)...)
				continue
			}
			out = append(out, r.blocks(c, ctx)...)
		}
	}
	return out
}

func (r *odtReader) paragraph(n *node, ctx *odtCtx) *Paragraph {
	styleName := n.attr("style-name")
	sr, sp, _ := r.style(styleName)
	para := &Paragraph{PageBreakBefore: sp.pageBreak}
	if sp.align != nil {
		para.Align = *sp.align
	}
	if sp.left != nil {
		para.LeftIndent = *sp.left
	}
	if sp.right != nil {
		para.RightIndent = *sp.right
	}
	if sp.first != nil {
		para.FirstIndent = *sp.first
	}
	if sp.before != nil {
		para.SpaceBefore = *sp.before
	}
	if sp.after != nil {
		para.SpaceAfter = *sp.after
	}
	if sp.line != nil {
		para.LineSpacing = *sp.line
	}
	if n.name == "h" {
		level := atoiDefault(n.attr("outline-level"), 1)
		para.Heading = min(6, max(1, level))
	} else if r.styleChain(styleName, func(s string) bool { return s == "quotations" || s == "quote" }) {
		para.Quote = true
	} else if r.styleChain(styleName, func(s string) bool {
		return s == "preformatted text" || s == "source text" || strings.Contains(s, "code")
	}) {
		para.Code = true
	}
	if ctx.inList && !ctx.header {
		ls := ctx.listStyle
		if ls == "" {
			ls = r.listStyleOf(styleName)
		}
		if ctx.firstItem {
			l := &List{Level: ctx.level, Ordered: true}
			start := 1
			if lv, ok := r.listStyles[ls][ctx.level]; ok {
				l.Ordered = !lv.bullet
				l.Format = lv.format
				start = lv.start
			} else if ls == "" {
				l.Ordered = false
			}
			if cnt := ctx.counter; cnt != nil {
				for len(cnt.n) <= ctx.level {
					cnt.n = append(cnt.n, 0)
				}
				for i := ctx.level + 1; i < len(cnt.n); i++ {
					cnt.n[i] = 0
				}
				switch {
				case ctx.startValue > 0:
					cnt.n[ctx.level] = ctx.startValue
				case cnt.n[ctx.level] == 0:
					cnt.n[ctx.level] = start
				default:
					cnt.n[ctx.level]++
				}
				if l.Ordered {
					l.Start = cnt.n[ctx.level] // every item gets its number
				}
			}
			para.List = l
			para.LeftIndent, para.FirstIndent = 0, 0
			ctx.firstItem, ctx.firstOfList = false, false
		} else {
			para.LeftIndent = listStep * float64(ctx.level+1)
			para.FirstIndent = 0
		}
	}
	var runs []Run
	r.inlines(n, sr, "", &runs)
	para.Runs = runs
	if para.Heading > 0 {
		// The heading's look comes back from its level.
		for i := range para.Runs {
			if para.Runs[i].Size == HeadingSize(para.Heading) {
				para.Runs[i].Size = 0
			}
		}
	}
	if sp.borderBottom && strings.TrimSpace(para.Text()) == "" && !hasImage(para) || isRuleText(para.Text()) {
		para.Rule, para.Runs, para.Align = true, nil, AlignLeft
	}
	return para
}

func (r *odtReader) runOf(text string, p rProps, link string) Run {
	return Run{Text: text, Font: p.font, Size: p.size, Bold: p.bold == yes, Italic: p.italic == yes,
		Underline: p.underline == yes && link == "", Strike: p.strike == yes, Super: p.super == yes,
		Sub: p.sub == yes, Color: p.color, Highlight: p.highlight, Link: link}
}

func (r *odtReader) inlines(n *node, props rProps, link string, runs *[]Run) {
	if props.hidden == yes {
		return
	}
	for _, c := range n.children {
		switch c.name {
		case "#text":
			*runs = append(*runs, r.runOf(collapseSpaces(c.text), props, link))
		case "span":
			sr, _, st := r.style(c.attr("style-name"))
			p := props
			if st != nil {
				p = st.r.over(props)
				_ = sr
			}
			r.inlines(c, p, link, runs)
		case "a":
			r.inlines(c, props, c.attr("href"), runs)
		case "s":
			*runs = append(*runs, r.runOf(strings.Repeat(" ", max(1, atoiDefault(c.attr("c"), 1))), props, link))
		case "tab":
			*runs = append(*runs, r.runOf("\t", props, link))
		case "line-break":
			*runs = append(*runs, r.runOf("\n", props, link))
		case "frame":
			if img := r.frameImage(c); img != nil {
				*runs = append(*runs, Run{Image: img, Link: link})
			}
		case "note", "annotation", "bookmark", "bookmark-start", "bookmark-end", "reference-mark",
			"soft-page-break", "change", "change-start", "change-end", "sequence-decls",
			"custom-shape", "rect", "ellipse", "line", "polyline", "polygon", "path", "g", "control", "connector":
			// left out
		default:
			r.inlines(c, props, link, runs)
		}
	}
}

// collapseSpaces applies the white space rule of ODF: runs of spaces,
// tabs and newlines in the XML are one space.
func collapseSpaces(s string) string {
	var b strings.Builder
	space := false
	for _, c := range s {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(c)
	}
	return b.String()
}

func (r *odtReader) frameImage(f *node) *Image {
	img := f.child("image")
	if img == nil {
		return nil
	}
	href := img.attr("href")
	if href == "" || strings.Contains(href, "://") {
		return nil
	}
	w, _ := parseLength(f.attr("width"))
	h, _ := parseLength(f.attr("height"))
	out := imageFromMedia(r.z.read(strings.TrimPrefix(href, "./")), w, h)
	if out != nil {
		if t := f.child("title"); t != nil {
			out.Alt = t.textContent()
		} else if d := f.child("desc"); d != nil {
			out.Alt = d.textContent()
		}
	}
	return out
}

func (r *odtReader) table(t *node) *Table {
	tbl := &Table{}
	var rows []*node
	var collect func(n *node, header bool)
	collect = func(n *node, header bool) {
		for _, c := range n.children {
			switch c.name {
			case "table-column":
				_, _, st := r.style(c.attr("style-name"))
				w := 0.0
				if st != nil {
					w = st.colWidth
				}
				for i := 0; i < max(1, atoiDefault(c.attr("number-columns-repeated"), 1)) && i < 1000; i++ {
					tbl.ColWidths = append(tbl.ColWidths, w)
				}
			case "table-columns", "table-column-group":
				collect(c, header)
			case "table-header-rows":
				if len(rows) == 0 {
					tbl.Header = true
				}
				collect(c, true)
			case "table-rows", "table-row-group":
				collect(c, header)
			case "table-row":
				for i := 0; i < max(1, atoiDefault(c.attr("number-rows-repeated"), 1)) && i < 1000; i++ {
					rows = append(rows, c)
				}
			}
		}
	}
	collect(t, false)
	for _, tr := range rows {
		var row Row
		spanned := 0 // covered cells still to come of a cell spanning columns
		for _, tc := range tr.children {
			switch tc.name {
			case "table-cell":
				rep := max(1, atoiDefault(tc.attr("number-columns-repeated"), 1))
				for i := 0; i < rep && i < 1000; i++ {
					cell := Cell{Span: atoiDefault(tc.attr("number-columns-spanned"), 1)}
					cell.Blocks = r.blocks(tc, odtCtx{})
					row.Cells = append(row.Cells, cell)
					spanned = max(0, cell.Span-1)
				}
			case "covered-table-cell":
				rep := max(1, atoiDefault(tc.attr("number-columns-repeated"), 1))
				for i := 0; i < rep && i < 1000; i++ {
					if spanned > 0 {
						spanned--
						continue
					}
					row.Cells = append(row.Cells, Cell{}) // covered by the cell above
				}
			}
		}
		tbl.Rows = append(tbl.Rows, row)
	}
	// Repeated empty rows and columns at the end are padding.
	for len(tbl.Rows) > 1 && rowEmpty(tbl.Rows[len(tbl.Rows)-1]) {
		tbl.Rows = tbl.Rows[:len(tbl.Rows)-1]
	}
	if len(tbl.Rows) == 0 {
		return nil
	}
	if n := tbl.Columns(); len(tbl.ColWidths) > n {
		tbl.ColWidths = tbl.ColWidths[:n]
	}
	return tbl
}

func rowEmpty(r Row) bool {
	for _, c := range r.Cells {
		var ps []*Paragraph
		flattenParagraphs(c.Blocks, &ps)
		for _, p := range ps {
			if len(p.Runs) > 0 {
				return false
			}
		}
	}
	return true
}
