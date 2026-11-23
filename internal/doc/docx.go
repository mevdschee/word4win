package doc

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// ReadDOCX reads a Word document (Office Open XML): paragraphs with their
// styles, headings, lists, tables, hyperlinks and pictures (PNG, JPEG, GIF
// and BMP). Headers, footers, footnotes, comments and tracked deletions are
// left out.
func ReadDOCX(data []byte) (*Document, error) {
	z, err := openZip(data)
	if err != nil {
		return nil, fmt.Errorf("not a Word document: %w", err)
	}
	body := z.xml("word/document.xml").find("body")
	if body == nil {
		return nil, fmt.Errorf("not a Word document: no word/document.xml")
	}
	r := &docxReader{z: z, doc: &Document{}, rels: map[string]rel{}, styles: map[string]*docxStyle{}}
	r.readRels()
	r.readTheme()
	r.readStyles()
	r.readNumbering()
	r.doc.Blocks = r.blocks(body)
	if len(r.doc.Blocks) == 0 {
		r.doc.Blocks = []Block{&Paragraph{}}
	}
	r.doc.Normalize()
	return r.doc, nil
}

type rel struct {
	target   string
	external bool
}

// tri is a property that may be unset.
type tri int8

const (
	unset tri = iota
	yes
	no
)

func (t tri) or(base tri) tri {
	if t != unset {
		return t
	}
	return base
}

type rProps struct {
	bold, italic, underline, strike, super, sub, hidden tri
	size                                                float64
	font                                                string
	color, highlight                                    *Color
	autoColor                                           bool
}

func (p rProps) over(base rProps) rProps {
	out := rProps{
		bold: p.bold.or(base.bold), italic: p.italic.or(base.italic), underline: p.underline.or(base.underline),
		strike: p.strike.or(base.strike), super: p.super.or(base.super), sub: p.sub.or(base.sub),
		hidden: p.hidden.or(base.hidden), size: base.size, font: base.font, color: base.color,
		highlight: base.highlight, autoColor: base.autoColor,
	}
	if p.size > 0 {
		out.size = p.size
	}
	if p.font != "" {
		out.font = p.font
	}
	if p.color != nil || p.autoColor {
		out.color, out.autoColor = p.color, p.autoColor
	}
	if p.highlight != nil {
		out.highlight = p.highlight
	}
	return out
}

type pProps struct {
	align                   *Align
	left, right, first      *float64
	before, after, line     *float64
	outline                 int // 1 to 9, 0 unset
	numID, ilvl             string
	pageBreak, borderBottom bool
	bidi                    tri
}

func (p pProps) over(base pProps) pProps {
	out := base
	if p.align != nil {
		out.align = p.align
	}
	for _, f := range []struct{ dst, src **float64 }{{&out.left, &p.left}, {&out.right, &p.right},
		{&out.first, &p.first}, {&out.before, &p.before}, {&out.after, &p.after}, {&out.line, &p.line}} {
		if *f.src != nil {
			*f.dst = *f.src
		}
	}
	if p.outline > 0 {
		out.outline = p.outline
	}
	if p.numID != "" {
		out.numID = p.numID
	}
	if p.ilvl != "" {
		out.ilvl = p.ilvl
	}
	out.pageBreak = out.pageBreak || p.pageBreak
	out.borderBottom = out.borderBottom || p.borderBottom
	out.bidi = p.bidi.or(base.bidi)
	return out
}

type docxStyle struct {
	name, basedOn, typ string
	r                  rProps
	p                  pProps
	resolving          bool
}

type numLevel struct {
	format string
	start  int
}

type docxReader struct {
	z      zipFiles
	doc    *Document
	rels   map[string]rel
	styles map[string]*docxStyle

	defaultR     rProps
	defaultP     pProps
	defaultStyle string
	majorFont    string
	minorFont    string

	abstract  map[string]map[int]numLevel // abstractNumId -> level
	nums      map[string]string           // numId -> abstractNumId
	overrides map[string]map[int]int      // numId -> level -> start override
	counters  map[string][]int            // abstractNumId -> the last number per level
	used      map[string]bool             // numId and level seen
}

func (r *docxReader) readRels() {
	root := r.z.xml("word/_rels/document.xml.rels")
	for _, n := range root.all("Relationship") {
		target := n.attr("Target")
		ext := strings.EqualFold(n.attr("TargetMode"), "External")
		if !ext && !strings.HasPrefix(target, "/") {
			target = path.Join("word", target)
		} else if !ext {
			target = strings.TrimPrefix(target, "/")
		}
		r.rels[n.attr("Id")] = rel{target: target, external: ext}
	}
}

func (r *docxReader) readTheme() {
	r.majorFont, r.minorFont = "Calibri Light", DefaultFont
	for id, rl := range r.rels {
		_ = id
		if strings.Contains(rl.target, "theme/theme") {
			theme := r.z.xml(rl.target)
			if f := theme.find("majorFont").child("latin"); f != nil && f.attr("typeface") != "" {
				r.majorFont = f.attr("typeface")
			}
			if f := theme.find("minorFont").child("latin"); f != nil && f.attr("typeface") != "" {
				r.minorFont = f.attr("typeface")
			}
		}
	}
}

func (r *docxReader) readStyles() {
	root := r.z.xml("word/styles.xml")
	if root == nil {
		return
	}
	if dd := root.child("docDefaults"); dd != nil {
		if rp := dd.child("rPrDefault").child("rPr"); rp != nil {
			r.defaultR = r.rProps(rp)
		}
		if pp := dd.child("pPrDefault").child("pPr"); pp != nil {
			r.defaultP = r.pProps(pp)
		}
	}
	for _, s := range root.all("style") {
		st := &docxStyle{typ: s.attr("type")}
		if n := s.child("name"); n != nil {
			st.name = n.attr("val")
		}
		if b := s.child("basedOn"); b != nil {
			st.basedOn = b.attr("val")
		}
		if rp := s.child("rPr"); rp != nil {
			st.r = r.rProps(rp)
		}
		if pp := s.child("pPr"); pp != nil {
			st.p = r.pProps(pp)
		}
		id := s.attr("styleId")
		r.styles[id] = st
		if st.typ == "paragraph" && (s.attr("default") == "1" || s.attr("default") == "true") {
			r.defaultStyle = id
		}
	}
}

// style resolves a style with the styles it is based on.
func (r *docxReader) style(id string) (rProps, pProps, string) {
	st := r.styles[id]
	if st == nil || st.resolving {
		return rProps{}, pProps{}, ""
	}
	st.resolving = true
	defer func() { st.resolving = false }()
	br, bp, _ := r.style(st.basedOn)
	return st.r.over(br), st.p.over(bp), st.name
}

func (r *docxReader) readNumbering() {
	r.abstract = map[string]map[int]numLevel{}
	r.nums = map[string]string{}
	r.overrides = map[string]map[int]int{}
	r.counters = map[string][]int{}
	r.used = map[string]bool{}
	root := r.z.xml("word/numbering.xml")
	for _, a := range root.all("abstractNum") {
		levels := map[int]numLevel{}
		for _, l := range a.all("lvl") {
			lv := numLevel{start: 1}
			if f := l.child("numFmt"); f != nil {
				lv.format = f.attr("val")
			}
			if s := l.child("start"); s != nil {
				lv.start = atoiDefault(s.attr("val"), 1)
			}
			levels[atoiDefault(l.attr("ilvl"), 0)] = lv
		}
		r.abstract[a.attr("abstractNumId")] = levels
	}
	for _, n := range root.all("num") {
		if a := n.child("abstractNumId"); a != nil {
			r.nums[n.attr("numId")] = a.attr("val")
		}
		for _, o := range n.all("lvlOverride") {
			if so := o.child("startOverride"); so != nil {
				if r.overrides[n.attr("numId")] == nil {
					r.overrides[n.attr("numId")] = map[int]int{}
				}
				r.overrides[n.attr("numId")][atoiDefault(o.attr("ilvl"), 0)] = atoiDefault(so.attr("val"), 1)
			}
		}
	}
}

func onOff(n *node) tri {
	if n == nil {
		return unset
	}
	switch strings.ToLower(n.attr("val")) {
	case "0", "false", "off", "none":
		return no
	}
	return yes
}

var highlightColors = map[string]Color{
	"yellow": {0xFF, 0xFF, 0x00}, "green": {0x00, 0xFF, 0x00}, "cyan": {0x00, 0xFF, 0xFF},
	"magenta": {0xFF, 0x00, 0xFF}, "blue": {0x00, 0x00, 0xFF}, "red": {0xFF, 0x00, 0x00},
	"darkBlue": {0x00, 0x00, 0x80}, "darkCyan": {0x00, 0x80, 0x80}, "darkGreen": {0x00, 0x80, 0x00},
	"darkMagenta": {0x80, 0x00, 0x80}, "darkRed": {0x80, 0x00, 0x00}, "darkYellow": {0x80, 0x80, 0x00},
	"darkGray": {0x80, 0x80, 0x80}, "lightGray": {0xC0, 0xC0, 0xC0}, "black": {0, 0, 0},
	"white": {0xFF, 0xFF, 0xFF},
}

func (r *docxReader) rProps(n *node) rProps {
	var p rProps
	p.bold = onOff(n.child("b"))
	p.italic = onOff(n.child("i"))
	if u := n.child("u"); u != nil {
		p.underline = yes
		if v := u.attr("val"); v == "none" || v == "0" {
			p.underline = no
		}
	}
	p.strike = onOff(n.child("strike")).or(onOff(n.child("dstrike")))
	p.hidden = onOff(n.child("vanish"))
	if va := n.child("vertAlign"); va != nil {
		switch va.attr("val") {
		case "superscript":
			p.super, p.sub = yes, no
		case "subscript":
			p.sub, p.super = yes, no
		default:
			p.super, p.sub = no, no
		}
	}
	if pos := n.child("position"); pos != nil && n.child("vertAlign") == nil {
		// Raised or lowered text, which Writer shows as super- and subscript.
		switch v := atoiDefault(pos.attr("val"), 0); {
		case v > 0:
			p.super, p.sub = yes, no
		case v < 0:
			p.sub, p.super = yes, no
		}
	}
	if sz := n.child("sz"); sz != nil {
		if v, err := strconv.ParseFloat(sz.attr("val"), 64); err == nil && v > 0 {
			p.size = v / 2
		}
	}
	if f := n.child("rFonts"); f != nil {
		switch {
		case f.attr("ascii") != "":
			p.font = f.attr("ascii")
		case f.attr("hAnsi") != "":
			p.font = f.attr("hAnsi")
		case strings.HasPrefix(f.attr("asciiTheme"), "major"):
			p.font = r.majorFont
		case strings.HasPrefix(f.attr("asciiTheme"), "minor"):
			p.font = r.minorFont
		}
	}
	if c := n.child("color"); c != nil {
		if v := c.attr("val"); v == "auto" {
			p.autoColor = true
		} else if col, ok := parseHexColor(v); ok {
			p.color = col
		}
	}
	if h := n.child("highlight"); h != nil {
		if c, ok := highlightColors[h.attr("val")]; ok {
			p.highlight = &c
		}
	} else if s := n.child("shd"); s != nil {
		if col, ok := parseHexColor(s.attr("fill")); ok && *col != (Color{0xFF, 0xFF, 0xFF}) {
			p.highlight = col
		}
	}
	return p
}

func twipsAttr(n *node, names ...string) *float64 {
	for _, name := range names {
		if v := n.attr(name); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				pt := f / twip
				return &pt
			}
		}
	}
	return nil
}

func (r *docxReader) pProps(n *node) pProps {
	var p pProps
	if jc := n.child("jc"); jc != nil {
		var a Align
		switch jc.attr("val") {
		case "center":
			a = AlignCenter
		case "right", "end":
			a = AlignRight
		case "both", "distribute", "lowKashida", "mediumKashida", "highKashida", "thaiDistribute":
			a = AlignJustify
		}
		p.align = &a
	}
	if ind := n.child("ind"); ind != nil {
		p.left = twipsAttr(ind, "left", "start")
		p.right = twipsAttr(ind, "right", "end")
		if h := twipsAttr(ind, "hanging"); h != nil {
			v := -*h
			p.first = &v
		} else {
			p.first = twipsAttr(ind, "firstLine")
		}
	}
	if sp := n.child("spacing"); sp != nil {
		p.before = twipsAttr(sp, "before")
		p.after = twipsAttr(sp, "after")
		if l := sp.attr("line"); l != "" {
			if v, err := strconv.ParseFloat(l, 64); err == nil {
				m := v / 240
				if rule := sp.attr("lineRule"); rule != "" && rule != "auto" {
					m = 0 // an exact or minimal height: no multiple
				}
				p.line = &m
			}
		}
	}
	if o := n.child("outlineLvl"); o != nil {
		if v := atoiDefault(o.attr("val"), 9); v < 9 {
			p.outline = v + 1
		}
	}
	if np := n.child("numPr"); np != nil {
		if id := np.child("numId"); id != nil {
			p.numID = id.attr("val")
			if p.numID == "" {
				p.numID = "0"
			}
		}
		if l := np.child("ilvl"); l != nil {
			p.ilvl = l.attr("val")
		}
	}
	p.pageBreak = onOff(n.child("pageBreakBefore")) == yes
	p.bidi = onOff(n.child("bidi"))
	if b := n.child("pBdr"); b != nil && b.child("bottom") != nil {
		p.borderBottom = true
	}
	return p
}

func (r *docxReader) blocks(parent *node) []Block {
	var out []Block
	for _, c := range parent.children {
		switch c.name {
		case "p":
			out = append(out, r.paragraphs(c)...)
		case "tbl":
			if t := r.table(c); t != nil {
				out = append(out, t)
			}
		case "sdt":
			out = append(out, r.blocks(c.child("sdtContent"))...)
		case "customXml", "ins", "smartTag":
			out = append(out, r.blocks(c)...)
		}
	}
	return out
}

// inline state while reading the runs of a paragraph.
type docxInline struct {
	runs      []Run
	link      string
	field     []string // instruction text of open complex fields
	fieldLink []string
	inResult  []bool
	pageBreak bool // a page break inside the paragraph
}

// paragraphs reads a w:p. A page break inside it splits it in two.
func (r *docxReader) paragraphs(p *node) []Block {
	pp := pProps{}
	styleID := r.defaultStyle
	var markR rProps
	if ppr := p.child("pPr"); ppr != nil {
		if ps := ppr.child("pStyle"); ps != nil {
			styleID = ps.attr("val")
		}
		pp = r.pProps(ppr)
		if rp := ppr.child("rPr"); rp != nil {
			markR = r.rProps(rp)
		}
	}
	_ = markR
	sr, sp, name := r.style(styleID)
	pp = pp.over(sp.over(r.defaultP))
	base := sr.over(r.defaultR)

	para := &Paragraph{PageBreakBefore: pp.pageBreak}
	if pp.align != nil {
		para.Align = *pp.align
	}
	if pp.bidi == yes {
		// Left and right are the start and end of a right-to-left paragraph.
		switch para.Align {
		case AlignLeft:
			para.Align = AlignRight
		case AlignRight:
			para.Align = AlignLeft
		}
	}
	if pp.left != nil {
		para.LeftIndent = *pp.left
	}
	if pp.right != nil {
		para.RightIndent = *pp.right
	}
	if pp.first != nil {
		para.FirstIndent = *pp.first
	}
	if pp.before != nil {
		para.SpaceBefore = *pp.before
	}
	if pp.after != nil {
		para.SpaceAfter = *pp.after
	}
	if pp.line != nil {
		para.LineSpacing = *pp.line
	}
	lname := strings.ToLower(name)
	switch {
	case pp.outline >= 1 && pp.outline <= 6:
		para.Heading = pp.outline
	case strings.HasPrefix(lname, "heading "):
		if lv := atoiDefault(strings.TrimPrefix(lname, "heading "), 0); lv >= 1 && lv <= 6 {
			para.Heading = lv
		}
	case lname == "quote" || lname == "intense quote":
		para.Quote = true
	}
	if pp.numID != "" && pp.numID != "0" {
		lvl := atoiDefault(pp.ilvl, 0)
		l := &List{Level: lvl, Ordered: true}
		lv, ok := r.abstract[r.nums[pp.numID]][atoiDefault(pp.ilvl, 0)]
		if !ok {
			lv = numLevel{format: "decimal", start: 1}
		}
		switch lv.format {
		case "bullet", "none":
			l.Ordered = false
		case "lowerLetter":
			l.Format = LowerAlpha
		case "upperLetter":
			l.Format = UpperAlpha
		case "lowerRoman":
			l.Format = LowerRoman
		case "upperRoman":
			l.Format = UpperRoman
		}
		// The items are numbered through other paragraphs as Writer does:
		// the lists (numIds) of one abstract numbering count on together,
		// and a list with a start override starts there when it is first
		// used. The model restarts after other paragraphs, so every item
		// gets its number.
		abs := r.nums[pp.numID]
		counters := r.counters[abs]
		for len(counters) <= lvl {
			counters = append(counters, 0)
		}
		for i := lvl + 1; i < len(counters); i++ {
			counters[i] = 0
		}
		key := pp.numID + ":" + strconv.Itoa(lvl)
		if start, ok := r.overrides[pp.numID][lvl]; ok && !r.used[key] {
			counters[lvl] = start
		} else if counters[lvl] == 0 {
			counters[lvl] = lv.start
		} else {
			counters[lvl]++
		}
		r.used[key] = true
		r.counters[abs] = counters
		if l.Ordered {
			l.Start = counters[lvl]
		}
		para.List = l
		para.LeftIndent, para.FirstIndent = 0, 0
	}

	in := &docxInline{}
	r.inlines(p, base, in)
	para.Runs = in.runs
	if para.Heading > 0 {
		// The heading style's look comes back from the heading level.
		for i := range para.Runs {
			if para.Runs[i].Size == HeadingSize(para.Heading) {
				para.Runs[i].Size = 0
			}
		}
	}
	if pp.borderBottom && strings.TrimSpace(para.Text()) == "" && !hasImage(para) || isRuleText(para.Text()) {
		para.Rule, para.Runs, para.Align = true, nil, AlignLeft
	}
	if !in.pageBreak {
		return []Block{para}
	}
	// Split at the page break markers.
	var out []Block
	cur := *para
	cur.Runs = nil
	for _, run := range para.Runs {
		if run.Text == "\f" {
			c := cur
			out = append(out, &c)
			cur.Runs, cur.PageBreakBefore, cur.List = nil, true, nil
			continue
		}
		cur.Runs = append(cur.Runs, run)
	}
	c := cur
	out = append(out, &c)
	// A paragraph that only had the break is not a paragraph of its own.
	if first, ok := out[0].(*Paragraph); ok && len(first.Runs) == 0 && len(out) > 1 && !first.PageBreakBefore {
		out = out[1:]
	}
	return out
}

func (r *docxReader) inlines(parent *node, base rProps, in *docxInline) {
	for _, c := range parent.children {
		switch c.name {
		case "r":
			r.run(c, base, in)
		case "hyperlink":
			old := in.link
			if id := c.attr("id"); id != "" {
				if rl, ok := r.rels[id]; ok {
					in.link = rl.target
				}
			} else if a := c.attr("anchor"); a != "" {
				in.link = "#" + a
			}
			r.inlines(c, base, in)
			in.link = old
		case "ins", "smartTag", "customXml", "fldSimple", "bdo", "dir":
			if c.name == "fldSimple" {
				if l := parseHyperlink(c.attr("instr")); l != "" {
					old := in.link
					in.link = l
					r.inlines(c, base, in)
					in.link = old
					continue
				}
			}
			r.inlines(c, base, in)
		case "sdt":
			r.inlines(c.child("sdtContent"), base, in)
		}
	}
}

func (r *docxReader) run(rn *node, base rProps, in *docxInline) {
	props := base
	if rpr := rn.child("rPr"); rpr != nil {
		direct := r.rProps(rpr)
		if rs := rpr.child("rStyle"); rs != nil {
			sr, _, _ := r.style(rs.attr("val"))
			props = sr.over(props)
		}
		props = direct.over(props)
	}
	if props.hidden == yes {
		return
	}
	mk := func(text string) Run {
		run := Run{Text: text, Font: props.font, Size: props.size, Bold: props.bold == yes,
			Italic: props.italic == yes, Underline: props.underline == yes, Strike: props.strike == yes,
			Super: props.super == yes, Sub: props.sub == yes, Color: props.color, Highlight: props.highlight}
		run.Link = in.link
		if len(in.fieldLink) > 0 && in.inResult[len(in.inResult)-1] && in.fieldLink[len(in.fieldLink)-1] != "" {
			run.Link = in.fieldLink[len(in.fieldLink)-1]
		}
		if run.Font == r.minorFont && r.minorFont == DefaultFont {
			run.Font = ""
		}
		return run
	}
	add := func(text string) {
		// Text inside a field's instruction is not shown.
		if len(in.field) > 0 && !in.inResult[len(in.inResult)-1] {
			in.field[len(in.field)-1] += text
			return
		}
		in.runs = append(in.runs, mk(text))
	}
	for _, c := range rn.children {
		switch c.name {
		case "t":
			add(c.textContent())
		case "instrText":
			if len(in.field) > 0 {
				in.field[len(in.field)-1] += c.textContent()
			}
		case "tab", "ptab":
			add("\t")
		case "br":
			if c.attr("type") == "page" {
				in.runs = append(in.runs, Run{Text: "\f"})
				in.pageBreak = true
			} else if c.attr("type") != "column" {
				add("\n")
			}
		case "cr":
			add("\n")
		case "noBreakHyphen":
			add("‑")
		case "sym":
			// A character of a symbol font, which has its glyphs in the
			// private use area: kept as it is, in that font.
			if v, err := strconv.ParseUint(c.attr("char"), 16, 32); err == nil {
				old := props.font
				if f := c.attr("font"); f != "" {
					props.font = f
				}
				add(string(rune(v)))
				props.font = old
			}
		case "fldChar":
			switch c.attr("fldCharType") {
			case "begin":
				in.field = append(in.field, "")
				in.fieldLink = append(in.fieldLink, "")
				in.inResult = append(in.inResult, false)
			case "separate":
				if n := len(in.field); n > 0 {
					in.fieldLink[n-1] = parseHyperlink(in.field[n-1])
					in.inResult[n-1] = true
				}
			case "end":
				if n := len(in.field); n > 0 {
					in.field, in.fieldLink, in.inResult = in.field[:n-1], in.fieldLink[:n-1], in.inResult[:n-1]
				}
			}
		case "drawing":
			if img := r.drawing(c); img != nil {
				run := mk("")
				run.Image = img
				in.runs = append(in.runs, run)
			}
		case "pict", "object":
			if img := r.vmlImage(c); img != nil {
				run := mk("")
				run.Image = img
				in.runs = append(in.runs, run)
			}
		}
	}
}

const emuPerPoint = 12700.0

func (r *docxReader) drawing(d *node) *Image {
	blip := d.find("blip")
	if blip == nil {
		return nil
	}
	id := blip.attr("embed")
	rl, ok := r.rels[id]
	if !ok || rl.external {
		return nil
	}
	var w, h float64
	if ext := d.find("extent"); ext != nil {
		w = float64(atoiDefault(ext.attr("cx"), 0)) / emuPerPoint
		h = float64(atoiDefault(ext.attr("cy"), 0)) / emuPerPoint
	}
	img := imageFromMedia(r.z.read(rl.target), w, h)
	if img != nil {
		if dp := d.find("docPr"); dp != nil {
			img.Alt = dp.attr("descr")
		}
	}
	return img
}

// vmlImage reads the old VML picture of a w:pict.
func (r *docxReader) vmlImage(p *node) *Image {
	data := p.find("imagedata")
	if data == nil {
		return nil
	}
	rl, ok := r.rels[data.attr("id")]
	if !ok || rl.external {
		return nil
	}
	var w, h float64
	if shape := p.find("shape"); shape != nil {
		for _, part := range strings.Split(shape.attr("style"), ";") {
			kv := strings.SplitN(part, ":", 2)
			if len(kv) != 2 {
				continue
			}
			v, ok := parseLength(kv[1])
			if !ok {
				continue
			}
			switch strings.TrimSpace(kv[0]) {
			case "width":
				w = v
			case "height":
				h = v
			}
		}
	}
	return imageFromMedia(r.z.read(rl.target), w, h)
}

func (r *docxReader) table(t *node) *Table {
	tbl := &Table{}
	if grid := t.child("tblGrid"); grid != nil {
		for _, gc := range grid.all("gridCol") {
			if w := twipsAttr(gc, "w"); w != nil {
				tbl.ColWidths = append(tbl.ColWidths, *w)
			} else {
				tbl.ColWidths = append(tbl.ColWidths, 0)
			}
		}
	}
	for _, tr := range t.all("tr") {
		var row Row
		header := false
		if trPr := tr.child("trPr"); trPr != nil && onOff(trPr.child("tblHeader")) == yes {
			header = true
		}
		for _, tc := range tr.all("tc") {
			cell := Cell{}
			if tcPr := tc.child("tcPr"); tcPr != nil {
				if gs := tcPr.child("gridSpan"); gs != nil {
					cell.Span = atoiDefault(gs.attr("val"), 1)
				}
				if vm := tcPr.child("vMerge"); vm != nil && vm.attr("val") != "restart" {
					row.Cells = append(row.Cells, cell) // covered by the cell above
					continue
				}
			}
			cell.Blocks = r.blocks(tc)
			row.Cells = append(row.Cells, cell)
		}
		if len(tbl.Rows) == 0 && header {
			tbl.Header = true
		}
		tbl.Rows = append(tbl.Rows, row)
	}
	if len(tbl.Rows) == 0 {
		return nil
	}
	return tbl
}
