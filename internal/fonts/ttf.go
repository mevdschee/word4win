// Package fonts reads TrueType fonts: their names and style for finding
// them, their metrics for laying out text, and their data for embedding in
// PDF. The layout engine, the PDF writer and the printer all measure text
// with these metrics, so the print preview, the printout and the PDF agree.
package fonts

import (
	"encoding/binary"
	"errors"
	"io"
	"sort"
	"unicode/utf16"
)

// Font is a parsed TrueType font.
type Font struct {
	Family    string // the name GDI knows it by (name ID 1)
	Subfamily string
	Bold      bool
	Italic    bool
	Path      string
	Index     int // in a collection

	UnitsPerEm                       int
	Ascent                           int // usWinAscent, positive
	Descent                          int // usWinDescent, positive
	UnderlinePos, UnderlineThickness int

	data     []byte // a standalone TTF (extracted from a collection)
	cmap     map[rune]uint16
	advances []uint16
	symbol   bool // a symbol font: characters are mapped at 0xF000
}

// Data returns the font as a standalone TrueType file, for embedding.
func (f *Font) Data() []byte { return f.data }

// GlyphIndex returns the glyph of a character, 0 when the font lacks it.
func (f *Font) GlyphIndex(r rune) uint16 {
	if g, ok := f.cmap[r]; ok {
		return g
	}
	if f.symbol && r < 0x100 {
		return f.cmap[0xF000+r]
	}
	return 0
}

// HasGlyph reports whether the font has a character.
func (f *Font) HasGlyph(r rune) bool { return f.GlyphIndex(r) != 0 }

// Advance returns the advance width of a character in em (1 is the font
// size).
func (f *Font) Advance(r rune) float64 {
	return float64(f.glyphAdvance(f.GlyphIndex(r))) / float64(f.UnitsPerEm)
}

func (f *Font) glyphAdvance(g uint16) uint16 {
	if len(f.advances) == 0 {
		return 0
	}
	if int(g) < len(f.advances) {
		return f.advances[g]
	}
	return f.advances[len(f.advances)-1]
}

// AscentEm and DescentEm are the line metrics in em, as GDI uses them.
func (f *Font) AscentEm() float64  { return float64(f.Ascent) / float64(f.UnitsPerEm) }
func (f *Font) DescentEm() float64 { return float64(f.Descent) / float64(f.UnitsPerEm) }

type table struct {
	tag            string
	offset, length uint32
}

var errFormat = errors.New("not a TrueType font")

// readTables reads the table directory of the font at offset.
func readTables(r io.ReaderAt, offset int64) (map[string]table, error) {
	var hdr [12]byte
	if _, err := r.ReadAt(hdr[:], offset); err != nil {
		return nil, err
	}
	version := binary.BigEndian.Uint32(hdr[0:])
	if version != 0x00010000 && version != 0x74727565 { // 1.0 and 'true'; 'OTTO' is CFF
		return nil, errFormat
	}
	n := int(binary.BigEndian.Uint16(hdr[4:]))
	if n == 0 || n > 200 {
		return nil, errFormat
	}
	dir := make([]byte, 16*n)
	if _, err := r.ReadAt(dir, offset+12); err != nil {
		return nil, err
	}
	tables := map[string]table{}
	for i := 0; i < n; i++ {
		e := dir[i*16:]
		tables[string(e[0:4])] = table{tag: string(e[0:4]), offset: binary.BigEndian.Uint32(e[8:]), length: binary.BigEndian.Uint32(e[12:])}
	}
	if _, ok := tables["glyf"]; !ok {
		return nil, errFormat // outlines in CFF cannot be embedded by the PDF writer
	}
	return tables, nil
}

func readTable(r io.ReaderAt, t table) ([]byte, error) {
	if t.length > 64<<20 {
		return nil, errFormat
	}
	b := make([]byte, t.length)
	_, err := r.ReadAt(b, int64(t.offset))
	return b, err
}

// fontOffsets returns the offsets of the fonts in a file: one for a TTF,
// one per font for a TTC.
func fontOffsets(r io.ReaderAt) ([]int64, error) {
	var hdr [12]byte
	if _, err := r.ReadAt(hdr[:], 0); err != nil {
		return nil, err
	}
	if string(hdr[0:4]) != "ttcf" {
		return []int64{0}, nil
	}
	n := int(binary.BigEndian.Uint32(hdr[8:]))
	if n <= 0 || n > 64 {
		return nil, errFormat
	}
	b := make([]byte, 4*n)
	if _, err := r.ReadAt(b, 12); err != nil {
		return nil, err
	}
	out := make([]int64, n)
	for i := range out {
		out[i] = int64(binary.BigEndian.Uint32(b[i*4:]))
	}
	return out, nil
}

// info is what the index needs of a font: names and style.
type info struct {
	family, subfamily, typoFamily string
	bold, italic                  bool
}

func readInfo(r io.ReaderAt, tables map[string]table) (info, error) {
	var in info
	name, ok := tables["name"]
	if !ok {
		return in, errFormat
	}
	nb, err := readTable(r, name)
	if err != nil {
		return in, err
	}
	names := parseNames(nb)
	in.family, in.subfamily, in.typoFamily = names[1], names[2], names[16]
	if in.family == "" {
		return in, errFormat
	}
	if os2, ok := tables["OS/2"]; ok {
		if b, err := readTable(r, os2); err == nil && len(b) >= 64 {
			sel := binary.BigEndian.Uint16(b[62:])
			in.italic = sel&1 != 0
			in.bold = sel&0x20 != 0
			return in, nil
		}
	}
	if head, ok := tables["head"]; ok {
		if b, err := readTable(r, head); err == nil && len(b) >= 46 {
			style := binary.BigEndian.Uint16(b[44:])
			in.bold, in.italic = style&1 != 0, style&2 != 0
		}
	}
	return in, nil
}

// parseNames reads the English Windows names of a name table, by name ID,
// falling back to the Macintosh Roman names.
func parseNames(b []byte) map[int]string {
	out := map[int]string{}
	if len(b) < 6 {
		return out
	}
	count := int(binary.BigEndian.Uint16(b[2:]))
	strOff := int(binary.BigEndian.Uint16(b[4:]))
	rank := map[int]int{}
	for i := 0; i < count; i++ {
		rec := 6 + i*12
		if rec+12 > len(b) {
			break
		}
		plat := binary.BigEndian.Uint16(b[rec:])
		enc := binary.BigEndian.Uint16(b[rec+2:])
		lang := binary.BigEndian.Uint16(b[rec+4:])
		id := int(binary.BigEndian.Uint16(b[rec+6:]))
		length := int(binary.BigEndian.Uint16(b[rec+8:]))
		off := strOff + int(binary.BigEndian.Uint16(b[rec+10:]))
		if off+length > len(b) {
			continue
		}
		raw := b[off : off+length]
		var s string
		score := 0
		switch {
		case plat == 3 && (enc == 1 || enc == 0 || enc == 10):
			u := make([]uint16, len(raw)/2)
			for k := range u {
				u[k] = binary.BigEndian.Uint16(raw[k*2:])
			}
			s = string(utf16.Decode(u))
			score = 2
			if lang == 0x409 {
				score = 3
			}
		case plat == 1 && enc == 0:
			s = string(raw) // close enough for the ASCII names that matter
			score = 1
		default:
			continue
		}
		if score > rank[id] {
			rank[id] = score
			out[id] = s
		}
	}
	return out
}

// parse reads the whole font at offset in data.
func parse(data []byte, offset int64) (*Font, error) {
	r := bytesReaderAt(data)
	tables, err := readTables(r, offset)
	if err != nil {
		return nil, err
	}
	in, err := readInfo(r, tables)
	if err != nil {
		return nil, err
	}
	f := &Font{Family: in.family, Subfamily: in.subfamily, Bold: in.bold, Italic: in.italic}
	get := func(tag string) []byte {
		t, ok := tables[tag]
		if !ok || int64(t.offset)+int64(t.length) > int64(len(data)) {
			return nil
		}
		return data[t.offset : t.offset+t.length]
	}
	head := get("head")
	if len(head) < 54 {
		return nil, errFormat
	}
	f.UnitsPerEm = int(binary.BigEndian.Uint16(head[18:]))
	if f.UnitsPerEm == 0 {
		return nil, errFormat
	}
	hhea := get("hhea")
	if len(hhea) < 36 {
		return nil, errFormat
	}
	f.Ascent = int(int16(binary.BigEndian.Uint16(hhea[4:])))
	f.Descent = -int(int16(binary.BigEndian.Uint16(hhea[6:])))
	numH := int(binary.BigEndian.Uint16(hhea[34:]))
	if os2 := get("OS/2"); len(os2) >= 78 {
		wa, wd := int(binary.BigEndian.Uint16(os2[74:])), int(binary.BigEndian.Uint16(os2[76:]))
		if wa+wd > 0 {
			f.Ascent, f.Descent = wa, wd
		}
	}
	if post := get("post"); len(post) >= 12 {
		f.UnderlinePos = int(int16(binary.BigEndian.Uint16(post[8:])))
		f.UnderlineThickness = int(int16(binary.BigEndian.Uint16(post[10:])))
	}
	if f.UnderlineThickness <= 0 {
		f.UnderlinePos, f.UnderlineThickness = -f.UnitsPerEm/10, f.UnitsPerEm/20
	}
	hmtx := get("hmtx")
	for i := 0; i < numH && i*4+2 <= len(hmtx); i++ {
		f.advances = append(f.advances, binary.BigEndian.Uint16(hmtx[i*4:]))
	}
	f.cmap, f.symbol = parseCmap(get("cmap"))
	if len(f.cmap) == 0 {
		return nil, errFormat
	}
	if offset == 0 {
		f.data = data
	} else {
		f.data = extract(data, tables)
	}
	return f, nil
}

// parseCmap reads the best Unicode character map: format 12 for all of
// Unicode, else format 4; a symbol font's map otherwise.
func parseCmap(b []byte) (map[rune]uint16, bool) {
	if len(b) < 4 {
		return nil, false
	}
	n := int(binary.BigEndian.Uint16(b[2:]))
	type sub struct {
		score  int
		off    int
		symbol bool
	}
	var best sub
	for i := 0; i < n; i++ {
		rec := 4 + i*8
		if rec+8 > len(b) {
			break
		}
		plat := binary.BigEndian.Uint16(b[rec:])
		enc := binary.BigEndian.Uint16(b[rec+2:])
		off := int(binary.BigEndian.Uint32(b[rec+4:]))
		if off+4 > len(b) {
			continue
		}
		format := binary.BigEndian.Uint16(b[off:])
		score := 0
		symbol := false
		switch {
		case plat == 3 && enc == 10 && format == 12:
			score = 5
		case plat == 0 && format == 12:
			score = 4
		case plat == 3 && enc == 1 && format == 4:
			score = 3
		case plat == 0 && format == 4:
			score = 2
		case plat == 3 && enc == 0 && format == 4:
			score, symbol = 1, true
		}
		if score > best.score {
			best = sub{score, off, symbol}
		}
	}
	if best.score == 0 {
		return nil, false
	}
	m := map[rune]uint16{}
	t := b[best.off:]
	switch binary.BigEndian.Uint16(t) {
	case 4:
		if len(t) < 14 {
			return nil, false
		}
		segs := int(binary.BigEndian.Uint16(t[6:])) / 2
		need := 16 + segs*8
		if len(t) < need {
			return nil, false
		}
		ends := t[14:]
		starts := t[16+segs*2:]
		deltas := t[16+segs*4:]
		ranges := t[16+segs*6:]
		for s := 0; s < segs; s++ {
			end := int(binary.BigEndian.Uint16(ends[s*2:]))
			start := int(binary.BigEndian.Uint16(starts[s*2:]))
			delta := int(int16(binary.BigEndian.Uint16(deltas[s*2:])))
			ro := int(binary.BigEndian.Uint16(ranges[s*2:]))
			if start > end || end == 0xFFFF && start == 0xFFFF {
				continue
			}
			for c := start; c <= end; c++ {
				var g int
				if ro == 0 {
					g = (c + delta) & 0xFFFF
				} else {
					pos := 16 + segs*6 + s*2 + ro + (c-start)*2
					if pos+2 > len(t) {
						continue
					}
					g = int(binary.BigEndian.Uint16(t[pos:]))
					if g != 0 {
						g = (g + delta) & 0xFFFF
					}
				}
				if g != 0 {
					m[rune(c)] = uint16(g)
				}
			}
		}
	case 12:
		if len(t) < 16 {
			return nil, false
		}
		groups := int(binary.BigEndian.Uint32(t[12:]))
		for gi := 0; gi < groups && 16+gi*12+12 <= len(t); gi++ {
			g := t[16+gi*12:]
			start := binary.BigEndian.Uint32(g)
			end := binary.BigEndian.Uint32(g[4:])
			gid := binary.BigEndian.Uint32(g[8:])
			if end-start > 0x30000 || end < start {
				continue
			}
			for c := start; c <= end; c++ {
				if v := gid + (c - start); v != 0 && v < 0x10000 {
					m[rune(c)] = uint16(v)
				}
			}
		}
	}
	return m, best.symbol
}

// extract makes a standalone TrueType file of one font of a collection.
func extract(data []byte, tables map[string]table) []byte {
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	n := len(tags)
	out := make([]byte, 12+16*n)
	binary.BigEndian.PutUint32(out[0:], 0x00010000)
	binary.BigEndian.PutUint16(out[4:], uint16(n))
	entry, log := 1, 0
	for entry*2 <= n {
		entry *= 2
		log++
	}
	binary.BigEndian.PutUint16(out[6:], uint16(entry*16))
	binary.BigEndian.PutUint16(out[8:], uint16(log))
	binary.BigEndian.PutUint16(out[10:], uint16(n*16-entry*16))
	for i, tag := range tags {
		t := tables[tag]
		if int64(t.offset)+int64(t.length) > int64(len(data)) {
			continue
		}
		body := data[t.offset : t.offset+t.length]
		off := len(out)
		out = append(out, body...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		e := out[12+i*16:]
		copy(e[0:4], tag)
		binary.BigEndian.PutUint32(e[4:], checksum(body))
		binary.BigEndian.PutUint32(e[8:], uint32(off))
		binary.BigEndian.PutUint32(e[12:], t.length)
	}
	return out
}

func checksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i < len(b); i += 4 {
		var w [4]byte
		copy(w[:], b[i:])
		sum += binary.BigEndian.Uint32(w[:])
	}
	return sum
}

type bytesReaderAt []byte

func (b bytesReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
