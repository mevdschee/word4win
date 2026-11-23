package doc

import (
	"bytes"
	"encoding/hex"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// ReadRTF parses RTF into a document. It understands what RichEdit and
// WordPad write, and most of what Word and LibreOffice write: character
// and paragraph formatting, lists (old \pn and Word's list tables), tables,
// hyperlinks, pictures (PNG, JPEG and bitmaps) and heading styles.
func ReadRTF(data []byte) (*Document, error) {
	return readRTF(data, false)
}

// readRTF reads RTF. Word's list items are numbered by their list
// definitions, as Writer and Word do; with listText by the number they
// were saved with (\listtext), which is what the writing program showed.
func readRTF(data []byte, listText bool) (*Document, error) {
	r := &rtfReader{data: data, doc: &Document{}, useListText: listText}
	// Wine's RichEdit writes \li and \fi swapped: the first line's indent
	// as \fi and the offset of the other lines as \li.
	r.wine = bytes.Contains(data[:min(len(data), 2048)], []byte(`{\*\generator Wine`))
	r.state = rtfState{uc: 1, dest: destNormal, para: paraProps{}, char: charProps{font: -1}}
	r.parse()
	r.flushText()
	if len(r.runs) > 0 { // text after the last \par
		r.endParagraph(0)
	}
	r.closeTables(0)
	if len(r.doc.Blocks) == 0 {
		r.doc.Blocks = []Block{&Paragraph{}}
	}
	r.doc.Normalize()
	detectHeadings(r.doc.Blocks)
	fixListStarts(r.doc.Blocks)
	return r.doc, nil
}

type destination int

const (
	destNormal destination = iota
	destSkip
	destFontTable
	destColorTable
	destStylesheet
	destFieldInst
	destFieldResult
	destPict
	destPn
	destListTable
	destListOverride
	destListText
	destShapeProp
)

type charProps struct {
	font                            int // index in the font table, -1 for the default
	size                            float64
	bold, italic, underline, strike bool
	super, sub, hidden              bool
	color, highlight                int // colour table indexes, 0 for none
	link                            string
}

type paraProps struct {
	align                      Align
	left, right, first         float64
	before, after, lineSpacing float64
	heading                    int
	itap                       int // table depth: 0 outside tables, 1 in a table, 2 in a nested one
	list                       *List
	ls, ilvl                   int // Word list override and level
	hasIlvl                    bool
	borderBottom               bool
	style                      int
}

type rtfState struct {
	dest destination
	uc   int
	char charProps
	para paraProps
	// The style being defined in the stylesheet, or the font in the font
	// table.
	defIndex int
	// In a \nesttableprops group: the row definition is of a nested table.
	nestProps bool
	// In a shape property: 1 in its name (\sn), 2 in its value (\sv).
	propPart int
}

type fontEntry struct {
	name    string
	charset int
}

type listLevel struct {
	nfc   int
	start int
}

type rtfReader struct {
	data  []byte
	pos   int
	state rtfState
	stack []rtfState
	doc   *Document

	fonts     map[int]*fontEntry
	defFont   int
	colors    []*Color
	colorCur  Color
	colorSet  bool
	styles    map[int]int // style index to heading level
	styleName strings.Builder
	lists     map[int][]listLevel // list id to levels
	overrides map[int]int         // ls to list id
	curListID int
	curLevels []listLevel
	curOverID int

	runs      []Run
	text      strings.Builder
	textChar  charProps
	pending   []byte // \'hh bytes of the current codepage, not decoded yet
	skipChars int    // characters to skip after \u
	surrogate rune

	fieldInst strings.Builder
	fieldLink string

	pict        pictState
	pageBreak   bool
	wine        bool
	listText    strings.Builder // the number of a Word list item, as shown
	useListText bool
	listCount   map[int][]int   // per Word list: the last number per level
	propName    strings.Builder // shape property name and value
	propValue   strings.Builder

	// Tables: the open tables, the outer one first, and the row
	// definitions per depth.
	tables   []*rtfTable
	defX     map[int][]float64 // right cell boundaries
	defMerge map[int][]int     // per cell: 0, 1 for \clmgf, 2 for \clmrg
	merge    int               // \clmgf or \clmrg of the cell being defined
	rowHdr   bool
}

// rtfTable is a table being read.
type rtfTable struct {
	t      *Table
	row    *Row
	cell   *Cell
	xs     [][]float64 // the cell boundaries of every row
	merges [][]int
}

type pictState struct {
	kind           string // "png", "jpeg", "dib", "wmf", "emf" or ""
	hex            strings.Builder
	picw, pich     int
	goalW, goalH   int
	scaleX, scaleY int
	alt            string
}

func (r *rtfReader) push() {
	r.stack = append(r.stack, r.state)
}

func (r *rtfReader) pop() {
	if len(r.stack) == 0 {
		return
	}
	old := r.state
	r.state = r.stack[len(r.stack)-1]
	r.stack = r.stack[:len(r.stack)-1]
	if old.dest == destPict && r.state.dest != destPict {
		r.endPict()
	}
	if old.dest == destFieldInst && r.state.dest != destFieldInst {
		r.fieldLink = parseHyperlink(r.fieldInst.String())
	}
	if old.dest == destFieldResult && r.state.dest != destFieldResult {
		r.fieldLink = ""
	}
	if old.dest == destStylesheet && r.state.dest == destStylesheet && old.defIndex >= 0 {
		r.endStyle(old.defIndex)
	}
	if old.dest == destShapeProp && old.propPart == 2 && r.state.propPart != 2 &&
		strings.TrimSpace(r.propName.String()) == "wzDescription" {
		r.pict.alt = r.propValue.String()
	}
	// Paragraph properties are not scoped by groups in practice: RichEdit
	// writes \pard inside a group and expects it to hold after it.
	body := func(d destination) bool { return d == destNormal || d == destFieldResult }
	if body(old.dest) && body(r.state.dest) {
		r.state.para = old.para
	}
}

func (r *rtfReader) parse() {
	for r.pos < len(r.data) {
		c := r.data[r.pos]
		switch c {
		case '{':
			r.pos++
			r.flushPending()
			r.push()
		case '}':
			r.pos++
			r.flushPending()
			if r.state.dest == destNormal || r.state.dest == destFieldResult {
				r.flushText()
			}
			r.pop()
		case '\\':
			r.pos++
			r.control()
		case '\r', '\n':
			r.pos++
		default:
			r.pos++
			r.char(c)
		}
	}
}

// control reads a control word or symbol after the backslash.
func (r *rtfReader) control() {
	if r.pos >= len(r.data) {
		return
	}
	c := r.data[r.pos]
	if !isLetter(c) {
		r.pos++
		r.symbol(c)
		return
	}
	start := r.pos
	for r.pos < len(r.data) && isLetter(r.data[r.pos]) {
		r.pos++
	}
	word := string(r.data[start:r.pos])
	param, hasParam := 0, false
	if r.pos < len(r.data) && (r.data[r.pos] == '-' || isDigit(r.data[r.pos])) {
		ps := r.pos
		r.pos++
		for r.pos < len(r.data) && isDigit(r.data[r.pos]) {
			r.pos++
		}
		param, _ = strconv.Atoi(string(r.data[ps:r.pos]))
		hasParam = true
	}
	if r.pos < len(r.data) && r.data[r.pos] == ' ' {
		r.pos++
	}
	if word == "bin" && hasParam { // binary data: skip it
		r.pos = min(len(r.data), r.pos+max(0, param))
		return
	}
	r.word(word, param, hasParam)
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }

func (r *rtfReader) symbol(c byte) {
	switch c {
	case '\\', '{', '}':
		r.char(c)
	case '\'':
		if r.pos+2 <= len(r.data) {
			if b, err := strconv.ParseUint(string(r.data[r.pos:r.pos+2]), 16, 8); err == nil {
				r.pos += 2
				if r.skipChars > 0 {
					r.skipChars--
					return
				}
				r.byteChar(byte(b))
				return
			}
		}
	case '*':
		// An ignorable destination: skipped, unless the next word is one
		// that is read.
		r.state.dest = destSkipCandidate(r)
	case '~':
		r.textRune(0xA0)
	case '_':
		r.textRune(0x2011)
	case '-':
		r.textRune(0xAD) // optional hyphen
	case '\n', '\r':
		r.word("par", 0, false)
	case '\t':
		r.textRune('\t')
	}
}

// destSkipCandidate looks at the control word after \* to decide whether
// the group is one that is read.
func destSkipCandidate(r *rtfReader) destination {
	rest := r.data[r.pos:]
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\r' || rest[0] == '\n') {
		rest = rest[1:]
	}
	if len(rest) > 1 && rest[0] == '\\' {
		end := 1
		for end < len(rest) && isLetter(rest[end]) {
			end++
		}
		switch string(rest[1:end]) {
		case "pn", "fldinst", "shppict", "listtable", "listoverridetable", "hyperlink", "nesttableprops":
			return r.state.dest // decided by the word itself
		case "picprop":
			if r.state.dest == destPict {
				return destShapeProp
			}
			return r.state.dest // decided by the word itself
		}
	}
	return destSkip
}

func (r *rtfReader) char(c byte) {
	switch r.state.dest {
	case destFontTable:
		if c == ';' {
			r.state.defIndex = -1
			return
		}
		if f := r.fonts[r.state.defIndex]; f != nil {
			f.name += string(c)
		}
		return
	case destColorTable:
		if c == ';' {
			if r.colorSet {
				col := r.colorCur
				r.colors = append(r.colors, &col)
			} else {
				r.colors = append(r.colors, nil)
			}
			r.colorCur, r.colorSet = Color{}, false
		}
		return
	case destStylesheet:
		if c != ';' {
			r.styleName.WriteByte(c)
		}
		return
	case destFieldInst:
		r.fieldInst.WriteByte(c)
		return
	case destPict:
		if isHex(c) {
			r.pict.hex.WriteByte(c)
		}
		return
	case destListText, destShapeProp:
		if c >= 0x20 {
			r.textRune(rune(c))
		}
		return
	case destNormal, destFieldResult:
	default:
		return
	}
	if c < 0x20 { // control characters are not text (a NUL often ends the data)
		return
	}
	if r.skipChars > 0 {
		r.skipChars--
		return
	}
	if c >= 0x80 {
		r.byteChar(c)
		return
	}
	r.flushPending()
	r.textRune(rune(c))
}

func isHex(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

// byteChar adds a byte of the font's code page.
func (r *rtfReader) byteChar(b byte) {
	switch r.state.dest {
	case destNormal, destFieldResult:
		r.pending = append(r.pending, b)
	case destFontTable:
		if f := r.fonts[r.state.defIndex]; f != nil {
			f.name += string(charmap.Windows1252.DecodeByte(b))
		}
	case destFieldInst, destListText, destShapeProp:
		r.textRune(charmap.Windows1252.DecodeByte(b))
	}
}

// flushPending decodes the pending code page bytes.
func (r *rtfReader) flushPending() {
	if len(r.pending) == 0 {
		return
	}
	data := r.pending
	r.pending = nil
	charset := 0
	if f := r.fonts[r.fontIndex()]; f != nil {
		charset = f.charset
	}
	if charset == 2 { // Symbol: map the common characters
		for _, b := range data {
			switch b {
			case 0xB7:
				r.textRune('•')
			default:
				r.textRune(rune(b))
			}
		}
		return
	}
	enc := charsetEncoding(charset)
	out, err := enc.NewDecoder().Bytes(data)
	if err != nil {
		out = []byte(string(charmap.Windows1252.DecodeByte(data[0])))
	}
	for _, ru := range string(out) {
		r.textRune(ru)
	}
}

func (r *rtfReader) fontIndex() int {
	if r.state.char.font >= 0 {
		return r.state.char.font
	}
	return r.defFont
}

func charsetEncoding(charset int) encoding.Encoding {
	switch charset {
	case 128:
		return japanese.ShiftJIS
	case 129:
		return korean.EUCKR
	case 134:
		return simplifiedchinese.GBK
	case 136:
		return traditionalchinese.Big5
	case 161:
		return charmap.Windows1253
	case 162:
		return charmap.Windows1254
	case 163:
		return charmap.Windows1258
	case 177:
		return charmap.Windows1255
	case 178:
		return charmap.Windows1256
	case 186:
		return charmap.Windows1257
	case 204:
		return charmap.Windows1251
	case 222:
		return charmap.Windows874
	case 238:
		return charmap.Windows1250
	}
	return charmap.Windows1252
}

// textRune adds a character to the text of the current run.
func (r *rtfReader) textRune(c rune) {
	switch r.state.dest {
	case destNormal, destFieldResult:
	case destFieldInst:
		r.fieldInst.WriteRune(c)
		return
	case destFontTable:
		if f := r.fonts[r.state.defIndex]; f != nil && c != ';' {
			f.name += string(c)
		}
		return
	case destListText:
		r.listText.WriteRune(c)
		return
	case destShapeProp:
		switch r.state.propPart {
		case 1:
			r.propName.WriteRune(c)
		case 2:
			r.propValue.WriteRune(c)
		}
		return
	default:
		return
	}
	if r.state.char.hidden || c < 0x20 && c != '\t' && c != '\n' {
		return
	}
	if r.surrogate != 0 {
		if c >= 0xDC00 && c <= 0xDFFF {
			c = utf16.DecodeRune(r.surrogate, c)
		}
		r.surrogate = 0
	} else if c >= 0xD800 && c <= 0xDBFF {
		r.surrogate = c
		return
	}
	cp := r.charProps()
	if r.text.Len() > 0 && cp != r.textChar {
		r.flushText()
	}
	r.textChar = cp
	r.text.WriteRune(c)
}

func (r *rtfReader) charProps() charProps {
	cp := r.state.char
	if r.fieldLink != "" && r.state.dest == destFieldResult {
		cp.link = r.fieldLink
	}
	return cp
}

// flushText ends the current run.
func (r *rtfReader) flushText() {
	r.flushPending()
	if r.text.Len() == 0 {
		return
	}
	cp := r.textChar
	run := Run{Text: r.text.String(), Bold: cp.bold, Italic: cp.italic, Underline: cp.underline,
		Strike: cp.strike, Super: cp.super, Sub: cp.sub, Link: cp.link}
	fi := cp.font
	if fi < 0 {
		fi = r.defFont
	}
	if f := r.fonts[fi]; f != nil {
		run.Font = strings.TrimSpace(f.name)
	}
	run.Size = cp.size
	// The colour table starts with the "auto" entry, which is nil.
	if cp.color > 0 && cp.color < len(r.colors) && r.colors[cp.color] != nil {
		c := *r.colors[cp.color]
		run.Color = &c
	}
	if cp.highlight > 0 && cp.highlight < len(r.colors) && r.colors[cp.highlight] != nil {
		c := *r.colors[cp.highlight]
		run.Highlight = &c
	}
	if cp.link != "" && run.Color != nil && *run.Color == (Color{0, 0, 255}) {
		run.Color = nil // the default link colour
	}
	r.runs = append(r.runs, run)
	r.text.Reset()
}

func (r *rtfReader) word(word string, param int, hasParam bool) {
	st := &r.state
	// Destinations first.
	switch word {
	case "fonttbl":
		st.dest = destFontTable
		st.defIndex = -1
		if r.fonts == nil {
			r.fonts = map[int]*fontEntry{}
		}
		return
	case "colortbl":
		st.dest = destColorTable
		return
	case "stylesheet":
		st.dest = destStylesheet
		st.defIndex = -1
		if r.styles == nil {
			r.styles = map[int]int{}
		}
		return
	case "fldinst":
		st.dest = destFieldInst
		r.fieldInst.Reset()
		return
	case "fldrslt":
		if st.dest != destSkip {
			r.flushText()
			st.dest = destFieldResult
		}
		return
	case "field":
		r.fieldLink = ""
		return
	case "pict":
		if st.dest == destNormal || st.dest == destFieldResult {
			r.flushText()
			st.dest = destPict
			r.pict = pictState{scaleX: 100, scaleY: 100}
		} else {
			st.dest = destSkip
		}
		return
	case "shppict":
		return
	case "nonshppict":
		st.dest = destSkip
		return
	case "pn":
		if st.dest == destNormal || st.dest == destFieldResult || st.dest == destSkip {
			st.dest = destPn
			if st.para.list == nil {
				st.para.list = &List{}
			}
			r.pnTarget().list = st.para.list
		}
		return
	case "listtable":
		st.dest = destListTable
		if r.lists == nil {
			r.lists = map[int][]listLevel{}
		}
		return
	case "listtext":
		if st.dest == destNormal {
			st.dest = destListText
			r.listText.Reset()
		} else {
			st.dest = destSkip
		}
		return
	case "nesttableprops":
		if st.dest == destNormal {
			st.nestProps = true
		}
		return
	case "listoverridetable":
		st.dest = destListOverride
		if r.overrides == nil {
			r.overrides = map[int]int{}
		}
		return
	case "info", "header", "footer", "headerl", "headerr", "headerf", "footerl", "footerr", "footerf",
		"footnote", "annotation", "xe", "tc", "txe", "rxe", "filetbl", "revtbl", "pgdsctbl",
		"objdata", "pntext", "nonesttables", "themedata", "colorschememapping", "latentstyles",
		"datastore", "xmlnstbl", "rsidtbl", "generator", "mmathPr", "fldtype", "bkmkstart", "bkmkend":
		st.dest = destSkip
		return
	}
	switch st.dest {
	case destSkip:
		return
	case destFontTable:
		r.fontWord(word, param)
		return
	case destColorTable:
		switch word {
		case "red":
			r.colorCur.R, r.colorSet = uint8(param), true
		case "green":
			r.colorCur.G, r.colorSet = uint8(param), true
		case "blue":
			r.colorCur.B, r.colorSet = uint8(param), true
		}
		return
	case destStylesheet:
		switch word {
		case "s":
			st.defIndex = param
			r.styleName.Reset()
		case "outlinelevel":
			if st.defIndex >= 0 && param >= 0 && param < 6 {
				r.styles[st.defIndex] = param + 1
			}
		}
		return
	case destPict:
		r.pictWord(word, param)
		return
	case destShapeProp:
		switch word {
		case "sn":
			st.propPart = 1
			r.propName.Reset()
		case "sv":
			st.propPart = 2
			r.propValue.Reset()
		case "u":
			ru := rune(param)
			if ru < 0 {
				ru += 65536
			}
			r.textRune(ru)
			r.skipChars = st.uc
		}
		return
	case destListText:
		if word == "tab" {
			return
		}
		r.symbolWord(word, param)
		if word == "u" {
			ru := rune(param)
			if ru < 0 {
				ru += 65536
			}
			r.textRune(ru)
			r.skipChars = st.uc
		}
		return
	case destPn:
		r.pnWord(word, param)
		return
	case destListTable:
		r.listWord(word, param)
		return
	case destListOverride:
		switch word {
		case "listid":
			r.curOverID = param
		case "ls":
			r.overrides[param] = r.curOverID
		}
		return
	case destFieldInst:
		r.symbolWord(word, param)
		return
	}

	// Body text.
	cp := &st.char
	pp := &st.para
	on := !hasParam || param != 0
	switch word {
	case "par":
		r.endParagraph(0)
	case "cell":
		r.endParagraph(1)
		r.endCell(1)
	case "nestcell":
		d := max(2, pp.itap)
		r.endParagraph(d)
		r.endCell(d)
	case "row":
		r.endRow(1)
	case "nestrow":
		r.endRow(max(2, len(r.tables)))
	case "trowd":
		d := r.defDepth()
		if r.defX == nil {
			r.defX, r.defMerge = map[int][]float64{}, map[int][]int{}
		}
		r.defX[d], r.defMerge[d], r.merge = nil, nil, 0
		if d == 1 {
			r.rowHdr = false
		}
	case "cellx":
		if r.defX == nil {
			r.defX, r.defMerge = map[int][]float64{}, map[int][]int{}
		}
		d := r.defDepth()
		r.defX[d] = append(r.defX[d], float64(param)/twip)
		r.defMerge[d] = append(r.defMerge[d], r.merge)
		r.merge = 0
	case "clmgf":
		r.merge = 1
	case "clmrg":
		r.merge = 2
	case "trhdr":
		r.rowHdr = true
	case "intbl":
		pp.itap = max(pp.itap, 1)
	case "itap":
		pp.itap = max(0, param)
	case "deleted":
		cp.hidden = on // a tracked deletion
	case "pard":
		*pp = paraProps{}
	case "plain":
		*cp = charProps{font: -1, link: cp.link}
	case "page":
		r.flushText()
		if len(r.runs) > 0 {
			r.endParagraph(0)
		}
		r.pageBreak = true
	case "pagebb":
		r.pageBreak = true
	case "line":
		r.textRune('\n')
	case "tab":
		r.textRune('\t')
	case "ql":
		pp.align = AlignLeft
	case "qc":
		pp.align = AlignCenter
	case "qr":
		pp.align = AlignRight
	case "qj", "qd":
		pp.align = AlignJustify
	case "li", "lin":
		pp.left = float64(param) / twip
	case "ri", "rin":
		pp.right = float64(param) / twip
	case "fi":
		pp.first = float64(param) / twip
	case "sb":
		pp.before = float64(param) / twip
	case "sa":
		pp.after = float64(param) / twip
	case "sl":
		if param > 0 {
			pp.lineSpacing = float64(param) / 240
		} else {
			pp.lineSpacing = 0
		}
	case "slmult":
		if param == 0 && pp.lineSpacing > 0 {
			pp.lineSpacing = 0 // an exact or minimal height, not a multiple
		}
	case "outlinelevel":
		if param >= 0 && param < 6 {
			pp.heading = param + 1
		}
	case "s":
		pp.style = param
	case "ls":
		pp.ls = param
	case "ilvl":
		pp.ilvl, pp.hasIlvl = param, true
	case "brdrb":
		pp.borderBottom = true
	case "f":
		cp.font = param
	case "fs":
		cp.size = float64(param) / 2
	case "b":
		cp.bold = on
	case "i":
		cp.italic = on
	case "ul", "uld", "uldash", "uldashd", "uldashdd", "uldb", "ulhwave", "ulldash", "ulth", "ulthd",
		"ulthdash", "ulw", "ulwave", "ululdbwave":
		cp.underline = on
	case "ulnone":
		cp.underline = false
	case "strike", "striked":
		cp.strike = on
	case "super":
		cp.super, cp.sub = on, false
	case "sub":
		cp.sub, cp.super = on, false
	case "nosupersub":
		cp.super, cp.sub = false, false
	case "up":
		cp.super = on
	case "dn":
		cp.sub = on
	case "v":
		cp.hidden = on
	case "cf":
		cp.color = param
	case "highlight", "cb", "chcbpat":
		cp.highlight = param
	case "deff":
		r.defFont = param
	case "uc":
		st.uc = param
	case "u":
		ru := rune(param)
		if ru < 0 {
			ru += 65536
		}
		r.flushPending()
		r.textRune(ru)
		r.skipChars = st.uc
	default:
		r.symbolWord(word, param)
	}
}

// symbolWord handles the control words that stand for a character.
func (r *rtfReader) symbolWord(word string, param int) {
	var c rune
	switch word {
	case "emdash":
		c = '—'
	case "endash":
		c = '–'
	case "bullet":
		c = '•'
	case "lquote":
		c = '‘'
	case "rquote":
		c = '’'
	case "ldblquote":
		c = '“'
	case "rdblquote":
		c = '”'
	case "emspace":
		c = 0x2003
	case "enspace":
		c = 0x2002
	case "qmspace":
		c = 0x2005
	case "zwj":
		c = 0x200D
	case "zwnj":
		c = 0x200C
	case "u":
		if r.state.dest == destFieldInst {
			ru := rune(param)
			if ru < 0 {
				ru += 65536
			}
			r.fieldInst.WriteRune(ru)
			r.skipChars = r.state.uc
		}
		return
	default:
		return
	}
	r.textRune(c)
}

func (r *rtfReader) fontWord(word string, param int) {
	switch word {
	case "f":
		r.state.defIndex = param
		if r.fonts[param] == nil {
			r.fonts[param] = &fontEntry{}
		}
	case "fcharset":
		if f := r.fonts[r.state.defIndex]; f != nil {
			f.charset = param
		}
	}
}

func (r *rtfReader) endStyle(index int) {
	name := strings.ToLower(strings.TrimSpace(r.styleName.String()))
	r.styleName.Reset()
	for level := 1; level <= 6; level++ {
		if name == "heading "+strconv.Itoa(level) || name == "heading"+strconv.Itoa(level) {
			if _, ok := r.styles[index]; !ok {
				r.styles[index] = level
			}
		}
	}
}

// pnTarget is the paragraph state that a \pn group changes: the one
// outside the group.
func (r *rtfReader) pnTarget() *paraProps {
	for i := len(r.stack) - 1; i >= 0; i-- {
		if r.stack[i].dest != destPn {
			return &r.stack[i].para
		}
	}
	return &r.state.para
}

func (r *rtfReader) pnWord(word string, param int) {
	l := r.pnTarget().list
	if l == nil {
		l = &List{}
		r.pnTarget().list = l
	}
	r.state.para.list = l
	switch word {
	case "pnlvlblt":
		l.Ordered = false
	case "pnlvlbody", "pnlvl", "pnlvlcont":
		l.Ordered = true
	case "pndec", "pnord", "pnordt", "pncard":
		l.Ordered, l.Format = true, Decimal
	case "pnlcltr":
		l.Ordered, l.Format = true, LowerAlpha
	case "pnucltr":
		l.Ordered, l.Format = true, UpperAlpha
	case "pnlcrm":
		l.Ordered, l.Format = true, LowerRoman
	case "pnucrm":
		l.Ordered, l.Format = true, UpperRoman
	case "pnstart":
		l.Start = param
	case "pntxta", "pntxtb":
		r.state.dest = destSkip
	}
}

func (r *rtfReader) listWord(word string, param int) {
	switch word {
	case "list":
		r.curLevels = nil
		r.curListID = 0
	case "listlevel":
		r.curLevels = append(r.curLevels, listLevel{start: 1})
	case "levelnfc", "levelnfcn":
		if n := len(r.curLevels); n > 0 {
			r.curLevels[n-1].nfc = param
		}
	case "levelstartat":
		if n := len(r.curLevels); n > 0 {
			r.curLevels[n-1].start = param
		}
	case "listid":
		r.curListID = param
		r.lists[param] = r.curLevels
	case "leveltext", "levelnumbers", "listname":
		r.state.dest = destSkip
	}
	if r.curListID != 0 {
		r.lists[r.curListID] = r.curLevels
	}
}

func (r *rtfReader) pictWord(word string, param int) {
	p := &r.pict
	switch word {
	case "pngblip":
		p.kind = "png"
	case "jpegblip":
		p.kind = "jpeg"
	case "dibitmap":
		p.kind = "dib"
	case "wmetafile":
		p.kind = "wmf"
	case "emfblip":
		p.kind = "emf"
	case "picw":
		p.picw = param
	case "pich":
		p.pich = param
	case "picwgoal":
		p.goalW = param
	case "pichgoal":
		p.goalH = param
	case "picscalex":
		p.scaleX = param
	case "picscaley":
		p.scaleY = param
	}
}

// endPict turns the picture that ended into an image run.
func (r *rtfReader) endPict() {
	p := &r.pict
	data, err := hex.DecodeString(evenHex(p.hex.String()))
	if err != nil || len(data) == 0 {
		return
	}
	img := &Image{}
	switch p.kind {
	case "png", "jpeg":
		img.Data, img.Format = data, p.kind
	case "dib":
		img.Data = dibToPNG(data)
	case "wmf":
		img.Data = dibToPNG(dibFromWMF(data))
	case "emf":
		img.Data = dibToPNG(dibFromEMF(data))
	}
	if img.Data == nil {
		return
	}
	if img.Format == "" {
		img.Format = "png"
	}
	img.Alt = p.alt
	if p.goalW > 0 && p.goalH > 0 {
		img.Width = float64(p.goalW) / twip * float64(max(1, p.scaleX)) / 100
		img.Height = float64(p.goalH) / twip * float64(max(1, p.scaleY)) / 100
	}
	r.runs = append(r.runs, Run{Image: img})
}

func evenHex(s string) string {
	if len(s)%2 == 1 {
		return s[:len(s)-1]
	}
	return s
}

func parseHyperlink(inst string) string {
	inst = strings.TrimSpace(inst)
	if !strings.HasPrefix(strings.ToUpper(inst), "HYPERLINK") {
		return ""
	}
	rest := strings.TrimSpace(inst[len("HYPERLINK"):])
	// Switches such as \l "anchor" or \o "tooltip" may come first.
	for strings.HasPrefix(rest, "\\") {
		sw := rest
		if i := strings.IndexByte(sw, ' '); i >= 0 {
			rest = strings.TrimSpace(sw[i:])
		} else {
			return ""
		}
		if strings.HasPrefix(sw, "\\l") {
			if a := quoted(rest); a != "" {
				return "#" + a
			}
		}
		if strings.HasPrefix(rest, "\"") && !strings.HasPrefix(sw, "\\l") {
			q := quoted(rest)
			rest = strings.TrimSpace(rest[len(q)+2:])
		}
	}
	if q := quoted(rest); q != "" {
		return q
	}
	if f := strings.Fields(rest); len(f) > 0 {
		return f[0]
	}
	return ""
}

func quoted(s string) string {
	if !strings.HasPrefix(s, "\"") {
		return ""
	}
	if end := strings.IndexByte(s[1:], '"'); end >= 0 {
		return s[1 : end+1]
	}
	return ""
}

// endParagraph ends the current paragraph. In a table (\intbl, or ended
// by \cell at table depth cellDepth) it goes into the cell.
func (r *rtfReader) endParagraph(cellDepth int) {
	r.flushText()
	pp := r.state.para
	listText := strings.TrimSpace(r.listText.String())
	r.listText.Reset()
	if r.wine {
		pp.left, pp.first = pp.first+pp.left, -pp.left
	}
	p := &Paragraph{
		Runs: r.runs, Align: pp.align, LeftIndent: pp.left, RightIndent: pp.right, FirstIndent: pp.first,
		SpaceBefore: pp.before, SpaceAfter: pp.after, LineSpacing: pp.lineSpacing, Heading: pp.heading,
		PageBreakBefore: r.pageBreak,
	}
	r.runs = nil
	if p.Heading == 0 && pp.style > 0 {
		p.Heading = r.styles[pp.style]
	}
	if pp.list != nil {
		l := *pp.list
		p.List = &l
		if pp.hasIlvl {
			p.List.Level = pp.ilvl
		}
	} else if pp.ls > 0 {
		if levels, ok := r.lists[r.overrides[pp.ls]]; ok {
			l := &List{Level: pp.ilvl, Ordered: true}
			if pp.ilvl < len(levels) {
				switch levels[pp.ilvl].nfc {
				case 23, 255:
					l.Ordered = false
				case 1:
					l.Format = UpperRoman
				case 2:
					l.Format = LowerRoman
				case 3:
					l.Format = UpperAlpha
				case 4:
					l.Format = LowerAlpha
				}
			}
			if l.Ordered {
				if r.useListText {
					l.Start = parseListNumber(listText, l.Format)
				}
				if l.Start == 0 {
					l.Start = r.nextNumber(r.overrides[pp.ls], pp.ilvl, levels)
				}
			}
			p.List = l
		} else {
			p.List = &List{Level: pp.ilvl}
		}
	}
	if p.List != nil {
		if pp.list != nil && !pp.hasIlvl {
			p.List.Level = max(0, int(math.Round(pp.left/listStep))-1)
		}
		p.LeftIndent, p.FirstIndent = 0, 0
		if pp.left != listStep*float64(p.List.Level+1) {
			p.LeftIndent = pp.left
		}
		if pp.first != -listHanging {
			p.FirstIndent = pp.first
		}
	}
	text := p.Text()
	if isRuleText(text) && len(p.Runs) > 0 || pp.borderBottom && strings.TrimSpace(text) == "" && !hasImage(p) {
		p.Rule, p.Runs, p.Align = true, nil, AlignLeft
	}
	r.pageBreak = false

	depth := max(pp.itap, cellDepth)
	if depth == 0 {
		r.closeTables(0)
		r.doc.Blocks = append(r.doc.Blocks, p)
		return
	}
	t := r.openTables(depth)
	t.cell.Blocks = append(t.cell.Blocks, p)
}

// nextNumber counts an item of a Word list: the lists number on through
// other paragraphs, from the start of their level.
func (r *rtfReader) nextNumber(list, level int, levels []listLevel) int {
	if r.listCount == nil {
		r.listCount = map[int][]int{}
	}
	c := r.listCount[list]
	for len(c) <= level {
		c = append(c, 0)
	}
	for i := level + 1; i < len(c); i++ {
		c[i] = 0
	}
	if c[level] == 0 {
		c[level] = 1
		if level < len(levels) && levels[level].start > 0 {
			c[level] = levels[level].start
		}
	} else {
		c[level]++
	}
	r.listCount[list] = c
	return c[level]
}

// parseListNumber reads the number of a list item from its shown text,
// such as "7." or "(c)"; 0 if there is none.
func parseListNumber(s string, f NumberFormat) int {
	s = strings.Trim(s, "().:- \t")
	if s == "" {
		return 0
	}
	if i := strings.LastIndexByte(s, '.'); i >= 0 { // "1.2." numbers the last level
		s = s[i+1:]
	}
	switch f {
	case LowerAlpha, UpperAlpha:
		n := 0
		for _, c := range strings.ToLower(s) {
			if c < 'a' || c > 'z' {
				return 0
			}
			n = n*26 + int(c-'a'+1)
		}
		return n
	case LowerRoman, UpperRoman:
		vals := map[rune]int{'i': 1, 'v': 5, 'x': 10, 'l': 50, 'c': 100, 'd': 500, 'm': 1000}
		n, prev := 0, 0
		rs := []rune(strings.ToLower(s))
		for i := len(rs) - 1; i >= 0; i-- {
			v := vals[rs[i]]
			if v == 0 {
				return 0
			}
			if v < prev {
				n -= v
			} else {
				n += v
				prev = v
			}
		}
		return n
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func hasImage(p *Paragraph) bool {
	for _, r := range p.Runs {
		if r.Image != nil {
			return true
		}
	}
	return false
}

func isRuleText(s string) bool {
	s = strings.TrimSpace(s)
	if len([]rune(s)) < 3 {
		return false
	}
	for _, c := range s {
		if c != '─' && c != '―' && c != '━' && c != '—' && c != '-' {
			return false
		}
	}
	return true
}

// defDepth is the depth of the table that a row definition (\trowd,
// \cellx) is for: a nested one in \nesttableprops, else the outer one.
func (r *rtfReader) defDepth() int {
	if r.state.nestProps {
		return max(2, len(r.tables))
	}
	return 1
}

// openTables makes sure tables are open down to depth, closing deeper
// ones, and returns the one at depth with an open row and cell.
func (r *rtfReader) openTables(depth int) *rtfTable {
	r.closeTables(depth)
	for len(r.tables) < depth {
		if n := len(r.tables); n > 0 {
			r.ensureCell(r.tables[n-1])
		}
		r.tables = append(r.tables, &rtfTable{t: &Table{}})
	}
	t := r.tables[depth-1]
	r.ensureCell(t)
	return t
}

func (r *rtfReader) ensureCell(t *rtfTable) {
	if t.row == nil {
		t.row = &Row{}
	}
	if t.cell == nil {
		t.cell = &Cell{}
	}
}

// closeTables ends the tables deeper than depth: a nested table goes into
// the cell around it, an outer one into the document.
func (r *rtfReader) closeTables(depth int) {
	for len(r.tables) > depth {
		n := len(r.tables)
		t := r.tables[n-1]
		if t.row != nil && len(t.row.Cells) > 0 {
			r.endRow(n)
		}
		r.tables = r.tables[:n-1]
		if len(t.t.Rows) == 0 {
			continue
		}
		finishTable(t)
		if n > 1 {
			parent := r.tables[n-2]
			r.ensureCell(parent)
			parent.cell.Blocks = append(parent.cell.Blocks, t.t)
		} else {
			r.doc.Blocks = append(r.doc.Blocks, t.t)
		}
	}
}

func (r *rtfReader) endCell(depth int) {
	t := r.openTables(depth)
	t.row.Cells = append(t.row.Cells, *t.cell)
	t.cell = nil
}

func (r *rtfReader) endRow(depth int) {
	r.flushText()
	if len(r.runs) > 0 {
		r.endParagraph(depth)
		r.endCell(depth)
	}
	if len(r.tables) < depth {
		return
	}
	r.closeTables(depth)
	t := r.tables[depth-1]
	if t.row == nil {
		return
	}
	if t.cell != nil && len(t.cell.Blocks) > 0 { // a cell without \cell
		t.row.Cells = append(t.row.Cells, *t.cell)
	}
	t.cell = nil
	if len(t.t.Rows) == 0 && depth == 1 {
		t.t.Header = r.rowHdr
	}
	t.t.Rows = append(t.t.Rows, *t.row)
	t.xs = append(t.xs, append([]float64(nil), r.defX[depth]...))
	t.merges = append(t.merges, append([]int(nil), r.defMerge[depth]...))
	t.row = nil
	r.state.para.itap = depth - 1
}

// finishTable works out the columns of a table from the cell boundaries
// of its rows: a cell that covers more columns gets a span.
func finishTable(rt *rtfTable) {
	t := rt.t
	for i := range t.Rows {
		cells, xs, merges := t.Rows[i].Cells, rt.xs[i], rt.merges[i]
		if len(xs) != len(cells) || len(merges) != len(cells) {
			continue
		}
		// \clmrg cells belong to the cell before them.
		var outCells []Cell
		var outX []float64
		for j, c := range cells {
			if merges[j] == 2 && len(outCells) > 0 {
				outX[len(outX)-1] = xs[j]
				continue
			}
			outCells = append(outCells, c)
			outX = append(outX, xs[j])
		}
		t.Rows[i].Cells, rt.xs[i] = outCells, outX
	}
	var bounds []float64
	for _, xs := range rt.xs {
		for _, x := range xs {
			found := false
			for _, b := range bounds {
				if math.Abs(b-x) < 1 {
					found = true
					break
				}
			}
			if !found {
				bounds = append(bounds, x)
			}
		}
	}
	if len(bounds) == 0 {
		return
	}
	sort.Float64s(bounds)
	prev := 0.0
	for _, b := range bounds {
		t.ColWidths = append(t.ColWidths, max(1, b-prev))
		prev = b
	}
	for i := range t.Rows {
		xs := rt.xs[i]
		if len(xs) != len(t.Rows[i].Cells) {
			continue
		}
		left := 0.0
		for j := range t.Rows[i].Cells {
			span := 0
			for _, b := range bounds {
				if b > left+1 && b < xs[j]+1 {
					span++
				}
			}
			if span > 1 {
				t.Rows[i].Cells[j].Span = span
			}
			left = xs[j]
		}
	}
}

// detectHeadings recognises the headings that this package writes, for
// RichEdit versions that do not keep the outline level: a paragraph of
// bold text in the size of a heading level.
func detectHeadings(blocks []Block) {
	for _, blk := range blocks {
		p, ok := blk.(*Paragraph)
		if !ok || p.Heading > 0 || p.List != nil || len(p.Runs) == 0 {
			continue
		}
		size := -1.0
		all := true
		for _, r := range p.Runs {
			if r.Image != nil || strings.TrimSpace(r.Text) == "" {
				continue
			}
			if !r.Bold || size >= 0 && r.Size != size {
				all = false
				break
			}
			size = r.Size
		}
		if !all || size < 0 {
			continue
		}
		for level := 1; level <= 6; level++ {
			if size == HeadingSize(level) {
				p.Heading = level
				break
			}
		}
	}
}

// ---- bitmaps in pictures ----

// dibToPNG converts a device independent bitmap (BITMAPINFOHEADER and
// pixels, as in \dibitmap) to PNG.
func dibToPNG(dib []byte) []byte {
	if len(dib) < 40 {
		return nil
	}
	img, err := decodeDIB(dib)
	if err != nil {
		return nil
	}
	var b bytes.Buffer
	if encodePNG(&b, img) != nil {
		return nil
	}
	return b.Bytes()
}

// dibFromWMF finds the bitmap in a Windows metafile, which is how Word and
// RichEdit store pasted pictures (META_STRETCHDIB or META_DIBSTRETCHBLT).
func dibFromWMF(data []byte) []byte {
	if len(data) < 18 {
		return nil
	}
	pos := int(le16(data[2:])) * 2 // header size in words
	for pos+6 <= len(data) {
		size := int(le32(data[pos:])) * 2
		fn := le16(data[pos+4:])
		if size < 6 || pos+size > len(data) {
			return nil
		}
		rec := data[pos : pos+size]
		switch fn {
		case 0x0F43: // META_STRETCHDIB: rop(4) usage(2) srcH srcW ySrc xSrc destH destW yDest xDest (2 each)
			if len(rec) > 6+4+2+16 {
				return rec[6+4+2+16:]
			}
		case 0x0B41: // META_DIBSTRETCHBLT: rop(4) srcH srcW ySrc xSrc destH destW yDest xDest
			if len(rec) > 6+4+16 {
				return rec[6+4+16:]
			}
		case 0x0940: // META_DIBBITBLT: rop(4) ySrc xSrc height width yDest xDest
			if len(rec) > 6+4+12 {
				return rec[6+4+12:]
			}
		case 0x0000:
			return nil
		}
		pos += size
	}
	return nil
}

// dibFromEMF finds the bitmap of an EMR_STRETCHDIBITS record in an
// enhanced metafile.
func dibFromEMF(data []byte) []byte {
	pos := 0
	for pos+8 <= len(data) {
		typ := le32(data[pos:])
		size := int(le32(data[pos+4:]))
		if size < 8 || pos+size > len(data) {
			return nil
		}
		rec := data[pos : pos+size]
		if typ == 81 && len(rec) >= 80 { // EMR_STRETCHDIBITS
			offBmi, cbBmi := int(le32(rec[48:])), int(le32(rec[52:]))
			offBits, cbBits := int(le32(rec[56:])), int(le32(rec[60:]))
			if offBmi+cbBmi <= len(rec) && offBits+cbBits <= len(rec) {
				dib := append([]byte{}, rec[offBmi:offBmi+cbBmi]...)
				return append(dib, rec[offBits:offBits+cbBits]...)
			}
		}
		if typ == 14 { // EMR_EOF
			return nil
		}
		pos += size
	}
	return nil
}

func le16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }
func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// fixListStarts keeps the start number only on the first item of a list:
// RichEdit may repeat it on every item.
func fixListStarts(blocks []Block) {
	var prev *Paragraph
	prevStart := 0
	for _, blk := range blocks {
		p, ok := blk.(*Paragraph)
		if !ok || p.List == nil {
			prev = nil
			continue
		}
		start := p.List.Start
		if prev != nil && start > 0 && start == prevStart {
			p.List.Start = 0
		}
		if prev == nil || p.List.Level <= prev.List.Level {
			prevStart = start
		}
		prev = p
	}
}
