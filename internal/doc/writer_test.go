package doc

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/gif"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writerBugs are differences that come from LibreOffice, not from word4win:
// per source, the text of a reference line that may differ.
var writerBugs = map[string][]string{
	// Writer, reading the RTF it wrote of the DOCX, puts the italic of the
	// second part of a link also on the first.
	"links-images.rtf": {"A link with "},
	// Writer ignores <del> and vertical-align, which browsers show as
	// struck through and raised, and a picture's width without a height,
	// and replaces the tabs in <pre> by spaces.
	"html.html": {"Deleted text: ", "Raised in CSS: ", "Scaled by its width only: ", "        with a tab"},
}

// writerHTMLDefaults takes out of what Writer made of an HTML page the
// formatting of Writer's own default styles, which word4win does not
// copy: the line spacing of "Text Body", the sizes of headings 3 to 6, the
// colour of "Internet Link" and the indent of "Quotations" (made the
// quote indent of word4win).
func writerHTMLDefaults(blocks []Block) {
	sizes := map[int]float64{3: 14, 4: 12, 5: 10, 6: 7}
	navy := Color{0, 0, 0x80}
	for _, blk := range blocks {
		switch v := blk.(type) {
		case *Paragraph:
			if math.Abs(v.LineSpacing-1.15) < 0.01 {
				v.LineSpacing = 0
			}
			if math.Abs(v.LeftIndent-28.35) < 0.1 {
				v.LeftIndent = listStep / 2
			}
			for i := range v.Runs {
				r := &v.Runs[i]
				if v.Heading > 0 && r.Size == sizes[v.Heading] {
					r.Size = 0
				}
				if r.Link != "" && r.Color != nil && *r.Color == navy {
					r.Color = nil
				}
			}
		case *Table:
			for _, row := range v.Rows {
				for _, c := range row.Cells {
					writerHTMLDefaults(c.Blocks)
				}
			}
		}
	}
}

// TestWriterReferences compares what word4win reads of the documents in
// testdata/writer with what LibreOffice Writer made of them, saved as
// <name>.writer.rtf by tools/genrefdocs (see the README there).
func TestWriterReferences(t *testing.T) {
	refs, _ := filepath.Glob("testdata/writer/*.writer.rtf")
	if len(refs) == 0 {
		t.Fatal("no Writer references in testdata/writer: run go run ./tools/genrefdocs")
	}
	for _, ref := range refs {
		src := strings.TrimSuffix(ref, ".writer.rtf")
		t.Run(filepath.Base(src), func(t *testing.T) {
			got := readForCompare(t, src)
			data, err := os.ReadFile(ref)
			if err != nil {
				t.Fatal(err)
			}
			// The numbers of list items are those Writer showed.
			want, err := readRTF(data, true)
			if err != nil {
				t.Fatal(err)
			}
			if f := rtfDefaultFont(data); f != "" {
				clearFont(want.Blocks, f)
			}
			if strings.HasSuffix(src, ".html") {
				writerHTMLDefaults(want.Blocks)
			}
			normalize(got)
			normalize(want)
			known := writerBugs[filepath.Base(src)]
			if g, w := dropKnown(got.PlainText(), want.PlainText(), known); g != w {
				t.Errorf("text differs:\n%s", firstDiff(g, w))
			}
			gh, err := WriteHTML(got, "", DataImageSaver)
			if err != nil {
				t.Fatal(err)
			}
			wh, err := WriteHTML(want, "", DataImageSaver)
			if err != nil {
				t.Fatal(err)
			}
			gs, ws := dropKnown(string(gh), string(wh), known)
			if gs != ws {
				dump(t, "word4win", gh)
				dump(t, "writer", wh)
				t.Errorf("formatting differs:\n%s", firstDiff(gs, ws))
			}
		})
	}
}

// rtfDefaultFont is the name of the default font (\deff) of an RTF file.
func rtfDefaultFont(data []byte) string {
	m := regexp.MustCompile(`\\deff(\d+)`).FindSubmatch(data)
	if m == nil {
		return ""
	}
	loc := regexp.MustCompile(`\{\\f` + string(m[1]) + `[\\ ]`).FindIndex(data)
	if loc == nil {
		return ""
	}
	var name strings.Builder
	for i := loc[1] - 1; i < len(data); i++ {
		c := data[i]
		switch {
		case c == ';' || c == '{' || c == '}':
			return strings.TrimSpace(name.String())
		case c == '\\':
			for i+1 < len(data) && (isLetter(data[i+1]) || isDigit(data[i+1]) || data[i+1] == '-') {
				i++
			}
			if i+1 < len(data) && data[i+1] == ' ' {
				i++
			}
		default:
			name.WriteByte(c)
		}
	}
	return ""
}

// clearFont makes runs in a document's default font use the default font:
// that of Writer's references is Writer's, not word4win's.
func clearFont(blocks []Block, font string) {
	for _, blk := range blocks {
		switch v := blk.(type) {
		case *Paragraph:
			for i := range v.Runs {
				if strings.EqualFold(v.Runs[i].Font, font) {
					v.Runs[i].Font = ""
				}
			}
		case *Table:
			for _, row := range v.Rows {
				for _, c := range row.Cells {
					clearFont(c.Blocks, font)
				}
			}
		}
	}
}

// normalize removes the differences that do not matter in a comparison:
// pictures are compared by their pixels (Writer converts GIF and BMP to
// PNG in its own way) and not by their alternative text (Writer's RTF does
// not keep a picture's title), and a table's header row by its bold text
// (Writer's RTF does not keep the "repeat as header" of a row).
func normalize(d *Document) {
	var blocks func([]Block)
	blocks = func(bs []Block) {
		for _, blk := range bs {
			switch v := blk.(type) {
			case *Paragraph:
				for i := range v.Runs {
					if img := v.Runs[i].Image; img != nil {
						c := *img
						c.Data, c.Format, c.Alt = pixelHash(img.Data), "png", ""
						v.Runs[i].Image = &c
					}
				}
			case *Table:
				if v.Header && len(v.Rows) > 0 {
					v.Header = false
					for _, c := range v.Rows[0].Cells {
						var paras []*Paragraph
						flattenParagraphs(c.Blocks, &paras)
						for _, p := range paras {
							for i := range p.Runs {
								p.Runs[i].Bold = true
							}
						}
					}
				}
				for _, row := range v.Rows {
					for _, c := range row.Cells {
						blocks(c.Blocks)
					}
				}
			}
		}
	}
	blocks(d.Blocks)
}

func pixelHash(data []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}
	h := sha256.New()
	b := img.Bounds()
	fmt.Fprintf(h, "%dx%d", b.Dx(), b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			fmt.Fprintf(h, "%d,%d,%d,%d;", r>>8, g>>8, bl>>8, a>>8)
		}
	}
	return h.Sum(nil)
}

// dropKnown leaves out the lines where the reference line contains one of
// the known texts, when both sides have the same number of lines.
func dropKnown(got, want string, known []string) (string, string) {
	if len(known) == 0 {
		return got, want
	}
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	if len(g) != len(w) {
		return got, want
	}
	var gk, wk []string
	for i := range w {
		skip := false
		for _, k := range known {
			if strings.Contains(w[i], k) {
				skip = true
			}
		}
		if !skip {
			gk, wk = append(gk, g[i]), append(wk, w[i])
		}
	}
	return strings.Join(gk, "\n"), strings.Join(wk, "\n")
}

// dump writes data to $WORD4WIN_DUMP/<test>.<side>.html when that is set,
// to compare the two sides of a failing test with a diff tool.
func dump(t *testing.T, side string, data []byte) {
	dir := os.Getenv("WORD4WIN_DUMP")
	if dir == "" {
		return
	}
	name := strings.NewReplacer("/", "_").Replace(t.Name()) + "." + side + ".html"
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Log(err)
	}
}

func readForCompare(t *testing.T, path string) *Document {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := ReadFile(path, data)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return d
}

// firstDiff shows the first line where got and want differ.
func firstDiff(got, want string) string { return firstDiffNamed(got, want, "word4win", "Writer") }

func firstDiffNamed(got, want, gotName, wantName string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return fmt.Sprintf("line %d\n  %-8s %s\n  %-8s %s", i+1, gotName+":", gl, wantName+":", wl)
		}
	}
	return "(no line differs)"
}
