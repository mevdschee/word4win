package doc

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// ---- reading ----

// ReadMarkdown parses CommonMark with the GitHub extensions (tables,
// strikethrough, task lists and bare links). Pictures are read from files
// relative to baseDir, or from data: URLs.
func ReadMarkdown(src []byte, baseDir string) *Document {
	md := goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough,
		extension.TaskList, extension.Linkify))
	root := md.Parser().Parse(text.NewReader(src))
	m := &mdReader{src: src, base: baseDir, doc: &Document{}}
	m.blocks(root, mdContext{})
	if len(m.doc.Blocks) == 0 {
		m.doc.Blocks = []Block{&Paragraph{}}
	}
	m.doc.Normalize()
	return m.doc
}

type mdReader struct {
	src  []byte
	base string
	doc  *Document
}

type mdContext struct {
	quote  bool
	inList bool
	level  int      // list nesting level, when inList
	out    *[]Block // where blocks go; nil is the document
}

func (m *mdReader) add(ctx mdContext, b Block) {
	if ctx.out != nil {
		*ctx.out = append(*ctx.out, b)
		return
	}
	m.doc.Blocks = append(m.doc.Blocks, b)
}

const paraSpace = 6.0

func (m *mdReader) blocks(n ast.Node, ctx mdContext) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		m.block(c, ctx)
	}
}

func (m *mdReader) block(n ast.Node, ctx mdContext) {
	switch v := n.(type) {
	case *ast.Heading:
		p := &Paragraph{Heading: v.Level, Quote: ctx.quote}
		p.Runs = m.inlines(v, Run{})
		m.add(ctx, p)
	case *ast.Paragraph, *ast.TextBlock:
		p := &Paragraph{Quote: ctx.quote, SpaceAfter: paraSpace}
		if ctx.inList {
			p.LeftIndent = listStep * float64(ctx.level+1)
			p.SpaceAfter = 0
		}
		p.Runs = m.inlines(n, Run{})
		m.add(ctx, p)
	case *ast.ThematicBreak:
		m.add(ctx, &Paragraph{Rule: true, SpaceAfter: paraSpace})
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		var lines []string
		segs := n.Lines()
		for i := 0; i < segs.Len(); i++ {
			s := segs.At(i)
			lines = append(lines, strings.TrimRight(string(s.Value(m.src)), "\r\n"))
		}
		if len(lines) == 0 {
			lines = []string{""}
		}
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
			m.add(ctx, p)
		}
	case *ast.Blockquote:
		inner := ctx
		inner.quote = true
		m.blocks(v, inner)
	case *ast.List:
		m.list(v, ctx)
	case *ast.HTMLBlock:
		var b strings.Builder
		segs := v.Lines()
		for i := 0; i < segs.Len(); i++ {
			s := segs.At(i)
			b.Write(s.Value(m.src))
		}
		for _, line := range strings.Split(strings.TrimRight(b.String(), "\r\n"), "\n") {
			p := &Paragraph{Quote: ctx.quote}
			if t := strings.TrimRight(line, "\r"); t != "" {
				p.Runs = []Run{{Text: t}}
			}
			m.add(ctx, p)
		}
	case *east.Table:
		m.table(v, ctx)
	default:
		if n.HasChildren() {
			m.blocks(n, ctx)
		}
	}
}

func (m *mdReader) list(l *ast.List, ctx mdContext) {
	level := 0
	if ctx.inList {
		level = ctx.level + 1
	}
	start := 0
	if l.IsOrdered() {
		start = l.Start
		if start == 0 {
			start = 1
		}
	}
	first := true
	for item := l.FirstChild(); item != nil; item = item.NextSibling() {
		inner := ctx
		inner.inList, inner.level = true, level
		firstChild := true
		for c := item.FirstChild(); c != nil; c = c.NextSibling() {
			switch c.(type) {
			case *ast.Paragraph, *ast.TextBlock:
				if firstChild {
					p := &Paragraph{Quote: ctx.quote, List: &List{Ordered: l.IsOrdered(), Level: level}}
					if first && l.IsOrdered() {
						p.List.Start = start
					}
					p.Runs = m.inlines(c, Run{})
					m.add(ctx, p)
					first, firstChild = false, false
					continue
				}
			}
			if firstChild { // an item that starts with something else
				p := &Paragraph{Quote: ctx.quote, List: &List{Ordered: l.IsOrdered(), Level: level}}
				if first && l.IsOrdered() {
					p.List.Start = start
				}
				m.add(ctx, p)
				first, firstChild = false, false
			}
			m.block(c, inner)
		}
		if firstChild { // an empty item
			p := &Paragraph{Quote: ctx.quote, List: &List{Ordered: l.IsOrdered(), Level: level}}
			m.add(ctx, p)
			first = false
		}
	}
	// Space after the outer list.
	if !ctx.inList {
		var blocks []Block
		if ctx.out != nil {
			blocks = *ctx.out
		} else {
			blocks = m.doc.Blocks
		}
		if n := len(blocks); n > 0 {
			if p, ok := blocks[n-1].(*Paragraph); ok && p.SpaceAfter == 0 {
				p.SpaceAfter = paraSpace
			}
		}
	}
}

func (m *mdReader) table(t *east.Table, ctx mdContext) {
	tbl := &Table{Header: true}
	for _, a := range t.Alignments {
		switch a {
		case east.AlignCenter:
			tbl.Aligns = append(tbl.Aligns, AlignCenter)
		case east.AlignRight:
			tbl.Aligns = append(tbl.Aligns, AlignRight)
		default:
			tbl.Aligns = append(tbl.Aligns, AlignLeft)
		}
	}
	for r := t.FirstChild(); r != nil; r = r.NextSibling() {
		var row Row
		for c := r.FirstChild(); c != nil; c = c.NextSibling() {
			p := &Paragraph{Runs: m.inlines(c, Run{})}
			row.Cells = append(row.Cells, Cell{Blocks: []Block{p}})
		}
		tbl.Rows = append(tbl.Rows, row)
	}
	tbl.Header = len(tbl.Rows) > 0
	m.add(ctx, tbl)
	m.add(ctx, &Paragraph{}) // RichEdit needs a paragraph between tables
}

var htmlTag = regexp.MustCompile(`^<(/?)([a-zA-Z]+)\s*/?>$`)

// inlines converts the inline children of a node to runs, starting from
// the style of base.
func (m *mdReader) inlines(n ast.Node, base Run) []Run {
	var runs []Run
	style := base
	var walk func(n ast.Node, st Run)
	walk = func(n ast.Node, st Run) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch v := c.(type) {
			case *ast.Text:
				r := st
				r.Text = string(v.Segment.Value(m.src))
				runs = append(runs, r)
				if v.HardLineBreak() {
					r.Text = "\n"
					runs = append(runs, r)
				} else if v.SoftLineBreak() {
					r.Text = " "
					runs = append(runs, r)
				}
			case *ast.String:
				r := st
				r.Text = string(v.Value)
				runs = append(runs, r)
			case *ast.CodeSpan:
				r := st
				var b strings.Builder
				for t := v.FirstChild(); t != nil; t = t.NextSibling() {
					if tx, ok := t.(*ast.Text); ok {
						b.Write(tx.Segment.Value(m.src))
					} else if s, ok := t.(*ast.String); ok {
						b.Write(s.Value)
					}
				}
				r.Text = b.String()
				r.Font, r.Size, r.Highlight = CodeFont, CodeSize, &codeBackground
				runs = append(runs, r)
			case *ast.Emphasis:
				s := st
				if v.Level >= 2 {
					s.Bold = true
				} else {
					s.Italic = true
				}
				walk(v, s)
			case *east.Strikethrough:
				s := st
				s.Strike = true
				walk(v, s)
			case *ast.Link:
				s := st
				s.Link = string(v.Destination)
				walk(v, s)
			case *ast.AutoLink:
				r := st
				url := string(v.URL(m.src))
				r.Text = string(v.Label(m.src))
				if v.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(url, "mailto:") {
					url = "mailto:" + url
				}
				r.Link = url
				runs = append(runs, r)
			case *ast.Image:
				if img := m.image(string(v.Destination)); img != nil {
					img.Alt = plainText(v, m.src)
					runs = append(runs, Run{Image: img, Link: st.Link})
				} else {
					r := st
					r.Text = plainText(v, m.src)
					runs = append(runs, r)
				}
			case *east.TaskCheckBox:
				r := st
				r.Text = "☐ "
				if v.IsChecked {
					r.Text = "☑ "
				}
				runs = append(runs, r)
			case *ast.RawHTML:
				var b strings.Builder
				for i := 0; i < v.Segments.Len(); i++ {
					s := v.Segments.At(i)
					b.Write(s.Value(m.src))
				}
				tag := b.String()
				if mt := htmlTag.FindStringSubmatch(tag); mt != nil {
					closing := mt[1] == "/"
					switch strings.ToLower(mt[2]) {
					case "br":
						r := st
						r.Text = "\n"
						runs = append(runs, r)
						continue
					case "u", "ins":
						st.Underline = !closing
						continue
					case "sup":
						st.Super = !closing
						continue
					case "sub":
						st.Sub = !closing
						continue
					case "b", "strong":
						st.Bold = !closing
						continue
					case "i", "em":
						st.Italic = !closing
						continue
					case "s", "del", "strike":
						st.Strike = !closing
						continue
					case "mark":
						if closing {
							st.Highlight = nil
						} else {
							st.Highlight = &Color{0xFF, 0xFF, 0x00}
						}
						continue
					}
				}
				r := st
				r.Text = tag
				runs = append(runs, r)
			default:
				walk(c, st)
			}
		}
	}
	walk(n, style)
	// Trailing hard breaks and spaces at the end of a paragraph are not text.
	for len(runs) > 0 {
		last := &runs[len(runs)-1]
		if last.Image != nil {
			break
		}
		t := strings.TrimRight(last.Text, " \n")
		if t != "" {
			last.Text = t
			break
		}
		runs = runs[:len(runs)-1]
	}
	return mergeRuns(runs)
}

func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch v := c.(type) {
			case *ast.Text:
				b.Write(v.Segment.Value(src))
			case *ast.String:
				b.Write(v.Value)
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}

// image loads a picture from a local file or a data: URL. Pictures on the
// web are not downloaded.
func (m *mdReader) image(dest string) *Image {
	var data []byte
	switch {
	case strings.HasPrefix(dest, "data:"):
		comma := strings.IndexByte(dest, ',')
		if comma < 0 || !strings.Contains(dest[:comma], ";base64") {
			return nil
		}
		d, err := base64.StdEncoding.DecodeString(dest[comma+1:])
		if err != nil {
			return nil
		}
		data = d
	case strings.Contains(dest, "://"):
		return nil
	default:
		path := strings.ReplaceAll(dest, "%20", " ")
		if !filepath.IsAbs(path) {
			if m.base == "" {
				return nil
			}
			path = filepath.Join(m.base, filepath.FromSlash(path))
		}
		d, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		data = d
	}
	img, err := LoadImage(data)
	if err != nil {
		return nil
	}
	return img
}

// ---- writing ----

// ImageSaver stores a picture of a Markdown document and returns the link
// to it.
type ImageSaver func(img *Image) (string, error)

// FileImageSaver saves pictures next to a Markdown file, named after the
// file and the picture's content, so that saving again writes nothing new.
func FileImageSaver(mdPath string) ImageSaver {
	dir := filepath.Dir(mdPath)
	base := strings.TrimSuffix(filepath.Base(mdPath), filepath.Ext(mdPath))
	return func(img *Image) (string, error) {
		sum := sha256.Sum256(img.Data)
		ext := ".png"
		if img.Format == "jpeg" {
			ext = ".jpg"
		}
		name := base + "-" + hex.EncodeToString(sum[:4]) + ext
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			if err := os.WriteFile(path, img.Data, 0o644); err != nil {
				return "", err
			}
		}
		return strings.ReplaceAll(name, " ", "%20"), nil
	}
}

// DataImageSaver embeds pictures as data: URLs.
func DataImageSaver(img *Image) (string, error) {
	mime := "image/png"
	if img.Format == "jpeg" {
		mime = "image/jpeg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data), nil
}

type mdWriter struct {
	b    strings.Builder
	save ImageSaver
	err  error
}

// WriteMarkdown converts a document to Markdown. What Markdown cannot
// express (fonts, sizes, colours, alignment) is left out. Text in a
// fixed-width font is code, an indented paragraph is a quote.
func WriteMarkdown(d *Document, save ImageSaver) ([]byte, error) {
	if save == nil {
		save = DataImageSaver
	}
	w := &mdWriter{save: save}
	w.blocks(d.Blocks)
	out := strings.TrimRight(w.b.String(), "\n") + "\n"
	if strings.TrimSpace(out) == "" {
		out = ""
	}
	return []byte(out), w.err
}

type mdKind int

const (
	kindNone mdKind = iota
	kindPara
	kindList
	kindCode
	kindQuote
	kindTable
	kindHeading
	kindRule
)

func isCodePara(p *Paragraph) bool {
	if p.Code {
		return true
	}
	if p.Heading > 0 || p.List != nil {
		return false
	}
	seen := false
	for _, r := range p.Runs {
		if r.Image != nil {
			return false
		}
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		if !IsMonospace(r.Font) {
			return false
		}
		seen = true
	}
	return seen
}

func isQuotePara(p *Paragraph) bool {
	return p.Quote || p.List == nil && p.Heading == 0 && !p.Rule && p.LeftIndent > 0 && p.FirstIndent >= -p.LeftIndent
}

func (w *mdWriter) blocks(blocks []Block) {
	nums := Numbering(blocks)
	prev := kindNone
	var listIndent []int // the indent of the text of each open list level
	lastOrdered := false
	blank := func() {
		if w.b.Len() > 0 {
			w.b.WriteString("\n")
		}
	}
	for i := 0; i < len(blocks); i++ {
		switch v := blocks[i].(type) {
		case *Table:
			if len(v.Rows) == 0 {
				continue
			}
			blank()
			w.table(v)
			prev = kindTable
		case *Paragraph:
			switch {
			case v.Rule:
				blank()
				w.b.WriteString("---\n")
				prev = kindRule
			case v.Heading > 0:
				blank()
				w.b.WriteString(strings.Repeat("#", v.Heading) + " ")
				w.b.WriteString(oneLine(w.inline(stripBold(v.Runs), true)))
				w.b.WriteString("\n")
				prev = kindHeading
			case v.List != nil:
				if prev != kindList || v.List.Level == 0 && lastOrdered != v.List.Ordered {
					blank()
					listIndent = listIndent[:0]
				}
				if v.List.Level == 0 {
					lastOrdered = v.List.Ordered
				}
				lvl := min(v.List.Level, len(listIndent))
				listIndent = listIndent[:lvl]
				indent := 0
				if lvl > 0 {
					indent = listIndent[lvl-1]
				}
				marker := "- "
				if v.List.Ordered {
					marker = fmt.Sprintf("%d. ", max(1, nums[v]))
				}
				runs := v.Runs
				if t := (&Paragraph{Runs: runs}).Text(); strings.HasPrefix(t, "☐ ") || strings.HasPrefix(t, "☑ ") {
					if strings.HasPrefix(t, "☑ ") {
						marker += "[x] "
					} else {
						marker += "[ ] "
					}
					runs = trimPrefix(runs, len("☐ "))
				}
				listIndent = append(listIndent, indent+len(marker))
				pad := strings.Repeat(" ", indent)
				text := w.inline(runs, false)
				text = strings.ReplaceAll(text, "\n", "\n"+pad+strings.Repeat(" ", len(marker)))
				w.b.WriteString(pad + marker + text + "\n")
				prev = kindList
			case prev == kindList && v.List == nil && !v.Quote && v.LeftIndent >= listStep && len(listIndent) > 0:
				// A further paragraph of a list item.
				lvl := min(len(listIndent)-1, max(0, int(v.LeftIndent/listStep+0.5)-1))
				pad := strings.Repeat(" ", listIndent[lvl])
				w.b.WriteString("\n")
				if isCodePara(v) {
					w.b.WriteString(pad + "    " + v.Text() + "\n")
				} else {
					text := w.inline(v.Runs, false)
					w.b.WriteString(pad + strings.ReplaceAll(escapeLineStart(text), "\n", "\n"+pad) + "\n")
				}
			case isCodePara(v):
				// A code block: this and the following code paragraphs.
				blank()
				var lines []string
				for ; i < len(blocks); i++ {
					p, ok := blocks[i].(*Paragraph)
					if !ok || !isCodePara(p) {
						break
					}
					lines = append(lines, p.Text())
				}
				i--
				body := strings.Join(lines, "\n")
				fence := "```"
				for strings.Contains(body, fence) {
					fence += "`"
				}
				w.b.WriteString(fence + "\n" + body + "\n" + fence + "\n")
				prev = kindCode
			case isQuotePara(v):
				if prev == kindQuote {
					w.b.WriteString(">\n")
				} else {
					blank()
				}
				text := w.inline(v.Runs, false)
				w.b.WriteString("> " + strings.ReplaceAll(text, "\n", "\n> ") + "\n")
				prev = kindQuote
			default:
				text := w.inline(v.Runs, false)
				if strings.TrimSpace(text) == "" && len(v.Runs) == 0 {
					// Empty paragraphs only separate blocks in Markdown.
					prev = kindNone
					continue
				}
				blank()
				w.b.WriteString(escapeLineStart(text) + "\n")
				prev = kindPara
			}
		}
	}
}

func oneLine(s string) string { return strings.ReplaceAll(s, "\\\n", " ") }

func stripBold(runs []Run) []Run {
	out := make([]Run, len(runs))
	for i, r := range runs {
		r.Bold = false
		out[i] = r
	}
	return out
}

// trimPrefix removes n bytes of text from the start of runs.
func trimPrefix(runs []Run, n int) []Run {
	out := append([]Run{}, runs...)
	for len(out) > 0 && n > 0 {
		if len(out[0].Text) <= n {
			n -= len(out[0].Text)
			out = out[1:]
			continue
		}
		out[0].Text = out[0].Text[n:]
		n = 0
	}
	return out
}

var (
	orderedStart = regexp.MustCompile(`^(\d+)([.)])(\s|$)`)
	bulletStart  = regexp.MustCompile(`^([-+*])(\s|$)`)
)

// escapeLineStart escapes what would make a paragraph a heading, list,
// quote or rule.
func escapeLineStart(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(line, "#"), strings.HasPrefix(line, ">"):
			line = "\\" + line
		case bulletStart.MatchString(line):
			line = "\\" + line
		case orderedStart.MatchString(line):
			m := orderedStart.FindStringSubmatchIndex(line)
			line = line[:m[3]] + "\\" + line[m[3]:]
		case strings.HasPrefix(line, "    "):
			line = "&#32;" + strings.TrimLeft(line, " ")
		case line == "---" || line == "===" || line == "***":
			line = "\\" + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// mdStyle is the part of a run's style that Markdown can express.
type mdStyle struct {
	bold, italic, strike, code, underline, super, sub bool
	link                                              string
}

func styleOf(r *Run) mdStyle {
	return mdStyle{bold: r.Bold, italic: r.Italic, strike: r.Strike, code: IsMonospace(r.Font),
		underline: r.Underline && r.Link == "", super: r.Super, sub: r.Sub, link: r.Link}
}

// inline renders runs as Markdown inline text. In a table cell line breaks
// become <br>.
func (w *mdWriter) inline(runs []Run, inTable bool) string {
	// Join runs that look the same in Markdown.
	type seg struct {
		st   mdStyle
		text string
		img  *Image
	}
	var segs []seg
	for i := range runs {
		r := &runs[i]
		if r.Image != nil {
			segs = append(segs, seg{st: mdStyle{link: r.Link}, img: r.Image})
			continue
		}
		st := styleOf(r)
		if n := len(segs); n > 0 && segs[n-1].img == nil && segs[n-1].st == st {
			segs[n-1].text += r.Text
			continue
		}
		segs = append(segs, seg{st: st, text: r.Text})
	}
	var b strings.Builder
	for _, s := range segs {
		if s.img != nil {
			url, err := w.save(s.img)
			if err != nil {
				if w.err == nil {
					w.err = err
				}
				continue
			}
			img := "![" + escapeInline(s.img.Alt) + "](" + url + ")"
			if s.st.link != "" {
				img = "[" + img + "](" + linkURL(s.st.link) + ")"
			}
			b.WriteString(img)
			continue
		}
		text := s.text
		// Spaces at the edges stay outside the markers.
		trimmed := strings.TrimLeft(text, " \t")
		lead := text[:len(text)-len(trimmed)]
		core := strings.TrimRight(trimmed, " \t")
		trail := trimmed[len(core):]
		if core == "" {
			b.WriteString(breaks(text, inTable))
			continue
		}
		var out string
		if s.st.code {
			out = codeSpan(core)
		} else {
			out = breaks(escapeInline(core), inTable)
		}
		if s.st.strike {
			out = "~~" + out + "~~"
		}
		switch {
		case s.st.bold && s.st.italic:
			out = "***" + out + "***"
		case s.st.bold:
			out = "**" + out + "**"
		case s.st.italic:
			out = "*" + out + "*"
		}
		if s.st.underline {
			out = "<u>" + out + "</u>"
		}
		if s.st.super {
			out = "<sup>" + out + "</sup>"
		} else if s.st.sub {
			out = "<sub>" + out + "</sub>"
		}
		if s.st.link != "" {
			if s.st.link == core && strings.Contains(core, "://") {
				out = "<" + core + ">"
			} else {
				out = "[" + out + "](" + linkURL(s.st.link) + ")"
			}
		}
		b.WriteString(breaks(lead, inTable) + out + breaks(trail, inTable))
	}
	return b.String()
}

func breaks(s string, inTable bool) string {
	if inTable {
		return strings.ReplaceAll(s, "\n", "<br>")
	}
	return strings.ReplaceAll(s, "\n", "\\\n")
}

func linkURL(u string) string {
	u = strings.ReplaceAll(u, " ", "%20")
	u = strings.ReplaceAll(u, "(", "%28")
	return strings.ReplaceAll(u, ")", "%29")
}

func codeSpan(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// escapeInline escapes the characters that would start inline markup.
func escapeInline(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		switch r {
		case '\\', '`', '*', '_', '[', ']', '~', '|':
			b.WriteRune('\\')
		case '<':
			if i+1 < len(rs) && (rs[i+1] == '/' || rs[i+1] == '!' || isASCIILetter(rs[i+1])) {
				b.WriteRune('\\')
			}
		case '&':
			// An entity like &amp; would be decoded.
			rest := string(rs[i+1:])
			if j := strings.IndexByte(rest, ';'); j > 0 && j < 10 && !strings.ContainsAny(rest[:j], " \t") {
				b.WriteString("&amp;")
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isASCIILetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

func (w *mdWriter) table(t *Table) {
	cols := t.Columns()
	if cols == 0 {
		return
	}
	cell := func(c *Cell, header bool) string {
		var parts []string
		var paras []*Paragraph
		flattenParagraphs(c.Blocks, &paras)
		for _, p := range paras {
			runs := p.Runs
			if header {
				runs = stripBold(runs)
			}
			if s := w.inline(runs, true); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "<br>")
	}
	row := func(r *Row, header bool) []string {
		out := make([]string, 0, cols)
		for i := range r.Cells {
			out = append(out, cell(&r.Cells[i], header))
			for s := 1; s < r.Cells[i].Span; s++ {
				out = append(out, "")
			}
		}
		for len(out) < cols {
			out = append(out, "")
		}
		return out[:cols]
	}
	write := func(cells []string) {
		w.b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	header := row(&t.Rows[0], true)
	write(header)
	sep := make([]string, cols)
	for i := range sep {
		align := AlignLeft
		if i < len(t.Aligns) {
			align = t.Aligns[i]
		} else if i < len(t.Rows[0].Cells) {
			if ps := t.Rows[0].Cells[i].Blocks; len(ps) > 0 {
				if p, ok := ps[0].(*Paragraph); ok {
					align = p.Align
				}
			}
		}
		switch align {
		case AlignCenter:
			sep[i] = ":---:"
		case AlignRight:
			sep[i] = "---:"
		default:
			sep[i] = "---"
		}
	}
	write(sep)
	for i := 1; i < len(t.Rows); i++ {
		write(row(&t.Rows[i], false))
	}
}

// IsMarkdownSafe reports whether a document survives a save as Markdown
// without losing formatting that shows: fonts, colours, sizes or
// alignment that Markdown cannot express.
func IsMarkdownSafe(d *Document) bool {
	var paras []*Paragraph
	flattenParagraphs(d.Blocks, &paras)
	for _, p := range paras {
		if p.Align == AlignCenter && !p.Rule || p.Align == AlignRight || p.Align == AlignJustify {
			return false
		}
		for _, r := range p.Runs {
			if r.Color != nil && *r.Color != quoteColor && *r.Color != ruleColor {
				return false
			}
			if r.Highlight != nil && *r.Highlight != codeBackground {
				return false
			}
			if r.Font != "" && !strings.EqualFold(r.Font, DefaultFont) && !IsMonospace(r.Font) {
				return false
			}
			if r.Size != 0 && r.Size != DefaultSize && r.Size != CodeSize && p.Heading == 0 &&
				!isHeadingSize(r.Size) {
				return false
			}
		}
	}
	return true
}

func isHeadingSize(size float64) bool {
	for level := 1; level <= 6; level++ {
		if size == HeadingSize(level) {
			return true
		}
	}
	return false
}
