// Command genrefdocs writes the test documents in
// internal/doc/testdata/writer: one DOCX per feature area, built here
// directly from WordprocessingML, the ODT and RTF that LibreOffice makes of
// each DOCX, and plain text files in several encodings. Then LibreOffice
// Writer converts every one of them to RTF as <name>.writer.rtf: that is
// the reference, word4win should read each document as Writer does (see
// the README in that directory and TestWriterReferences).
//
// Run from the repository root; it needs soffice (LibreOffice) on the PATH:
//
//	go run ./tools/genrefdocs
package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding/charmap"
)

const outDir = "internal/doc/testdata/writer"

func main() {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatal(err)
	}
	docs := []struct {
		name string
		make func(*docx)
	}{
		{"text", textDoc},
		{"paragraphs", paragraphsDoc},
		{"lists", listsDoc},
		{"tables", tablesDoc},
		{"links-images", linksImagesDoc},
		{"styles", stylesDoc},
		{"extras", extrasDoc},
	}
	var written []string
	for _, d := range docs {
		x := newDocx()
		d.make(x)
		file := filepath.Join(outDir, d.name+".docx")
		if err := os.WriteFile(file, x.bytes(), 0o644); err != nil {
			log.Fatal(err)
		}
		written = append(written, file)
	}
	writeTexts()
	writeMarkdownHTML()
	if _, err := exec.LookPath("soffice"); err != nil {
		log.Fatal("soffice (LibreOffice) is not on the PATH")
	}
	for _, ext := range []string{"odt", "rtf"} {
		soffice(ext, outDir, "", written...)
	}
	writeReferences()
}

// soffice converts files with LibreOffice into dir. infilter, when not
// empty, says how to read them.
func soffice(to, dir, infilter string, files ...string) {
	args := []string{"--headless", "--convert-to", to, "--outdir", dir}
	if infilter != "" {
		args = append(args, "--infilter="+infilter)
	}
	cmd := exec.Command("soffice", append(args, files...)...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("soffice %s: %v", to, err)
	}
}

// writeReferences lets Writer convert every source to RTF, as
// <name>.<ext>.writer.rtf. soffice names its output after the source
// without the extension, so each group goes through its own directory.
// Markdown has no reference: LibreOffice 24 cannot read it.
func writeReferences() {
	old, _ := filepath.Glob(filepath.Join(outDir, "*.writer.rtf"))
	for _, f := range old {
		os.Remove(f)
	}
	tmp, err := os.MkdirTemp("", "genrefdocs")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	glob := func(pattern string) []string {
		files, _ := filepath.Glob(filepath.Join(outDir, pattern))
		return files
	}
	var texts, cp1252 []string
	for _, f := range glob("*.txt") {
		if strings.Contains(f, "cp1252") {
			cp1252 = append(cp1252, f) // without a filter Writer reads it as UTF-8
		} else {
			texts = append(texts, f)
		}
	}
	groups := []struct {
		infilter string
		files    []string
	}{
		{"", glob("*.docx")},
		{"", glob("*.odt")},
		{"", glob("*.rtf")},
		{"", texts},
		{"Text (encoded):MS_1252,CRLF,,", cp1252},
		{"HTML (StarWriter)", glob("*.html")}, // in Writer, not Writer/Web
	}
	for i, g := range groups {
		dir := filepath.Join(tmp, fmt.Sprint(i))
		soffice("rtf", dir, g.infilter, g.files...)
		for _, src := range g.files {
			base := filepath.Base(src)
			from := filepath.Join(dir, strings.TrimSuffix(base, filepath.Ext(base))+".rtf")
			if err := os.Rename(from, filepath.Join(outDir, base+".writer.rtf")); err != nil {
				log.Fatal(err)
			}
		}
	}
}

// Text files: the same text in several encodings and line ends.
func writeTexts() {
	lines := []string{
		"Plain text reference",
		"",
		"Accents: café, naïve, Ærøskøbing, Straße, señor.",
		"Symbols: € £ © ® ™ ° ± × ÷ “quotes” ‘single’ – en — em …",
		"Tab\tseparated\tcolumns",
		"\tIndented with a tab",
		"    Indented with four spaces",
		"",
		"A long line that goes on and on so that it has to be wrapped by the editor when word wrap is on, " +
			"and that stays one line when word wrap is off, to see how the window and the saved file treat it.",
		"Last line without a line end",
	}
	ascii := []string{
		"Plain text reference",
		"",
		"Only ASCII here, so every encoding reads it the same.",
		"Tab\tseparated\tcolumns",
		"Last line",
	}
	greek := "Greek: αβγ, Cyrillic: жзи, CJK: 漢字, emoji: 😀"
	join := func(ls []string, eol string) string { return strings.Join(ls, eol) }
	utf8Lines := append(append([]string{}, lines[:4]...), append([]string{greek}, lines[4:]...)...)
	cp1252, err := charmap.Windows1252.NewEncoder().String(join(lines, "\r\n"))
	if err != nil {
		log.Fatal(err)
	}
	files := map[string][]byte{
		"text-ascii-lf.txt":    []byte(join(ascii, "\n")),
		"text-utf8-crlf.txt":   []byte(join(utf8Lines, "\r\n")),
		"text-utf8-lf.txt":     []byte(join(utf8Lines, "\n")),
		"text-utf8-bom.txt":    append([]byte{0xEF, 0xBB, 0xBF}, join(utf8Lines, "\r\n")...),
		"text-utf16le-bom.txt": utf16Bytes(join(utf8Lines, "\r\n"), binary.LittleEndian, []byte{0xFF, 0xFE}),
		"text-utf16be-bom.txt": utf16Bytes(join(utf8Lines, "\r\n"), binary.BigEndian, []byte{0xFE, 0xFF}),
		"text-cp1252-crlf.txt": []byte(cp1252),
		"text-empty-lines.txt": []byte("\r\n\r\nTwo empty lines above, two below.\r\n\r\n\r\n"),
	}
	for _, name := range sortedKeys(files) {
		data := files[name]
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func utf16Bytes(s string, order binary.AppendByteOrder, bom []byte) []byte {
	b := append([]byte{}, bom...)
	for _, u := range utf16.Encode([]rune(s)) {
		b = order.AppendUint16(b, u)
	}
	return b
}

// --- documents ---

const lorem = "The editor wraps this paragraph over several lines so that the alignment is visible: " +
	"the quick brown fox jumps over the lazy dog, and the lazy dog does not seem to mind at all, " +
	"while the text goes on long enough to fill at least three lines on an A4 page."

func textDoc(d *docx) {
	d.add(p(pStyle("Heading1"), t("Character formatting")))
	d.add(p("", t("Plain, "), r("<w:b/>", "bold"), t(", "), r("<w:i/>", "italic"), t(", "),
		r(`<w:u w:val="single"/>`, "underline"), t(", "), r("<w:strike/>", "strikethrough"), t(", "),
		r(`<w:b/><w:i/><w:u w:val="single"/>`, "bold italic underline"), t(".")))
	d.add(p("", t("Underlines: "), r(`<w:u w:val="double"/>`, "double"), t(", "),
		r(`<w:u w:val="dotted"/>`, "dotted"), t(", "), r(`<w:u w:val="dash"/>`, "dashed"), t(", "),
		r(`<w:u w:val="wave"/>`, "wavy"), t(", "), r(`<w:u w:val="words"/>`, "words only"), t(", "),
		r(`<w:u w:val="thick"/>`, "thick"), t(".")))
	d.add(p("", t("Other: "), r("<w:dstrike/>", "double strike"), t(", "), r("<w:smallCaps/>", "Small Caps"), t(", "),
		r("<w:caps/>", "all caps"), t(", hidden text after this colon:"), r("<w:vanish/>", " HIDDEN"), t(", "),
		r("<w:outline/>", "outline"), t(", "), r("<w:shadow/>", "shadow"), t(", "), r(`<w:spacing w:val="60"/>`, "expanded"), t(".")))
	d.add(p("", t("Scripts: E = mc"), r(`<w:vertAlign w:val="superscript"/>`, "2"), t(", H"),
		r(`<w:vertAlign w:val="subscript"/>`, "2"), t("O, x"), r(`<w:position w:val="6"/>`, "raised"),
		t(" and x"), r(`<w:position w:val="-6"/>`, "lowered"), t(" by position.")))
	d.add(p(pStyle("Heading2"), t("Fonts")))
	for _, f := range []string{"Times New Roman", "Arial", "Courier New", "Georgia", "Verdana", "Segoe UI", "Comic Sans MS", "Cambria Math", "No Such Font"} {
		d.add(p("", r(font(f), "The quick brown fox jumps over the lazy dog in "+f+".")))
	}
	d.add(p("", t("Symbol font: "), r(font("Symbol"), "abgd"), t(", Wingdings: "), r(font("Wingdings"), "J K L")))
	d.add(p(pStyle("Heading2"), t("Sizes")))
	var runs []string
	for _, hp := range []int{12, 16, 20, 22, 24, 28, 36, 48, 72, 96} {
		runs = append(runs, r(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, hp, hp), fmt.Sprintf("%gpt ", float64(hp)/2)))
	}
	runs = append(runs, r(`<w:sz w:val="21"/>`, "10.5pt"))
	d.add(p("", runs...))
	d.add(p(pStyle("Heading2"), t("Colours")))
	runs = nil
	for _, c := range []struct{ name, hex string }{
		{"red", "FF0000"}, {"green", "00B050"}, {"blue", "0070C0"}, {"brown", "8B4513"}, {"grey", "808080"}, {"white on black", "FFFFFF"},
	} {
		rpr := `<w:color w:val="` + c.hex + `"/>`
		if c.hex == "FFFFFF" {
			rpr += `<w:shd w:val="clear" w:color="auto" w:fill="000000"/>`
		}
		runs = append(runs, r(rpr, c.name), t(", "))
	}
	runs = append(runs, r(`<w:color w:val="auto"/>`, "automatic"), t(", "),
		r(`<w:color w:val="4472C4" w:themeColor="accent1"/>`, "theme accent 1"), t("."))
	d.add(p("", runs...))
	runs = []string{t("Highlights: ")}
	for _, h := range []string{"yellow", "green", "cyan", "magenta", "lightGray", "darkBlue"} {
		rpr := `<w:highlight w:val="` + h + `"/>`
		if h == "darkBlue" {
			rpr += `<w:color w:val="FFFFFF"/>`
		}
		runs = append(runs, r(rpr, h), t(" "))
	}
	runs = append(runs, t("and shading "), r(`<w:shd w:val="clear" w:color="auto" w:fill="FFC000"/>`, "orange fill"), t("."))
	d.add(p("", runs...))
	d.add(p(pStyle("Heading2"), t("Characters")))
	d.add(p("", t("Accents: café, naïve, Ærøskøbing, Straße, señor, Ångström.")))
	d.add(p("", t("Symbols: € £ ¥ © ® ™ ° ± × ÷ § ¶ • “double” ‘single’ «guillemets» – en — em … ½ ¼.")))
	d.add(p("", t("Scripts: Greek αβγδ, Cyrillic Москва, Hebrew שלום, Arabic مرحبا, CJK 漢字かなカナ 한글, Thai ไทย.")))
	d.add(p("", t("Beyond the BMP: 😀 🎉 𝄞 𝔸.")))
	d.add(p("", t("Non-breaking space, soft­hyphen, non-breaking"), `<w:r><w:noBreakHyphen/></w:r>`, t("hyphen, zero​width space.")))
	d.add(p("", t("A line break"), `<w:r><w:br/></w:r>`, t("inside a paragraph, and a"), `<w:r><w:tab/></w:r>`, t("tab.")))
	d.add(p("", t("Symbol characters with w:sym: "), `<w:r><w:sym w:font="Wingdings" w:char="F04A"/></w:r>`,
		t(" "), `<w:r><w:sym w:font="Symbol" w:char="F0E5"/></w:r>`, t(".")))
}

func paragraphsDoc(d *docx) {
	d.add(p(pStyle("Heading1"), t("Paragraph formatting")))
	for _, a := range []string{"left", "center", "right", "both"} {
		d.add(p(`<w:jc w:val="`+a+`"/>`, r("<w:b/>", "Aligned "+a+". "), t(lorem)))
	}
	d.add(p(`<w:ind w:left="1440"/>`, r("<w:b/>", "Left indent 1 inch. "), t(lorem)))
	d.add(p(`<w:ind w:right="1440"/>`, r("<w:b/>", "Right indent 1 inch. "), t(lorem)))
	d.add(p(`<w:ind w:left="720" w:right="720"/>`, r("<w:b/>", "Both indents half an inch. "), t(lorem)))
	d.add(p(`<w:ind w:firstLine="720"/>`, r("<w:b/>", "First line indent half an inch. "), t(lorem)))
	d.add(p(`<w:ind w:left="720" w:hanging="720"/>`, r("<w:b/>", "Hanging indent half an inch. "), t(lorem)))
	d.add(p(`<w:ind w:left="-360"/>`, r("<w:b/>", "Negative left indent. "), t("Starts in the margin.")))
	d.add(p(pStyle("Heading2"), t("Spacing")))
	d.add(p(`<w:spacing w:before="480" w:after="240"/>`, t("24pt before and 12pt after.")))
	d.add(p(`<w:spacing w:before="0" w:after="0"/>`, t("No space before or after (1).")))
	d.add(p(`<w:spacing w:before="0" w:after="0"/>`, t("No space before or after (2).")))
	for _, ls := range []struct{ name, attrs string }{
		{"Single line spacing", `w:line="240" w:lineRule="auto"`},
		{"1.15 line spacing", `w:line="276" w:lineRule="auto"`},
		{"1.5 line spacing", `w:line="360" w:lineRule="auto"`},
		{"Double line spacing", `w:line="480" w:lineRule="auto"`},
		{"Exactly 18pt line spacing", `w:line="360" w:lineRule="exact"`},
		{"At least 24pt line spacing", `w:line="480" w:lineRule="atLeast"`},
	} {
		d.add(p(`<w:spacing w:after="120" `+ls.attrs+`/>`, r("<w:b/>", ls.name+". "), t(lorem)))
	}
	d.add(p(pStyle("Heading2"), t("Tabs")))
	tabs := `<w:tabs><w:tab w:val="left" w:pos="1440"/><w:tab w:val="center" w:pos="4320"/><w:tab w:val="right" w:pos="7200"/><w:tab w:val="decimal" w:pos="8640"/></w:tabs>`
	tab := `<w:r><w:tab/></w:r>`
	d.add(p(tabs, t("Name"), tab, t("Left"), tab, t("Center"), tab, t("Right"), tab, t("Decimal")))
	d.add(p(tabs, t("One"), tab, t("a"), tab, t("middle"), tab, t("end"), tab, t("1.5")))
	d.add(p(tabs, t("Two"), tab, t("abc"), tab, t("centred text"), tab, t("flush right"), tab, t("123.25")))
	d.add(p(`<w:tabs><w:tab w:val="right" w:leader="dot" w:pos="8640"/></w:tabs>`, t("Dot leader"), tab, t("42")))
	d.add(p("", t("Default"), tab, t("tab"), tab, t("stops"), tab, t("every half inch.")))
	d.add(p(pStyle("Heading2"), t("Borders and shading")))
	d.add(p(`<w:pBdr><w:top w:val="single" w:sz="8" w:space="1" w:color="000000"/><w:left w:val="single" w:sz="8" w:space="4" w:color="000000"/><w:bottom w:val="single" w:sz="8" w:space="1" w:color="000000"/><w:right w:val="single" w:sz="8" w:space="4" w:color="000000"/></w:pBdr>`,
		t("A paragraph with a box around it.")))
	d.add(p(`<w:shd w:val="clear" w:color="auto" w:fill="DDEBF7"/>`, t("A paragraph with a light blue background.")))
	d.add(p(pStyle("Heading2"), t("Breaks")))
	d.add(p("", t("A page break follows this text."), `<w:r><w:br w:type="page"/></w:r>`, t("This text is on the next page.")))
	d.add(p(`<w:pageBreakBefore/>`, t("This paragraph has page break before.")))
	d.add(p(`<w:bidi/><w:jc w:val="left"/>`, t("Right-to-left paragraph: שלום עולם.")))
}

func listsDoc(d *docx) {
	d.add(p(pStyle("Heading1"), t("Lists")))
	li := func(n, lvl int, text string) string { return p(pStyle("ListParagraph")+numPr(n, lvl), t(text)) }
	d.add(p(pStyle("Heading2"), t("Bullets")))
	d.add(li(1, 0, "First bullet"), li(1, 1, "Second level"), li(1, 2, "Third level"), li(1, 1, "Back to the second level"), li(1, 0, "Second bullet"))
	d.add(p("", t("A paragraph between lists.")))
	d.add(p(pStyle("Heading2"), t("Numbers, letters and Roman numerals")))
	d.add(li(2, 0, "One"), li(2, 1, "One a"), li(2, 1, "One b"), li(2, 2, "One b i"), li(2, 2, "One b ii"), li(2, 0, "Two"), li(2, 0, "Three"))
	d.add(p("", t("Upper case letters:")))
	d.add(li(3, 0, "Alpha"), li(3, 0, "Bravo"), li(3, 0, "Charlie"))
	d.add(p("", t("Upper case Roman numerals:")))
	d.add(li(4, 0, "First"), li(4, 0, "Second"), li(4, 0, "Third"), li(4, 0, "Fourth"))
	d.add(p("", t("Numbers with a parenthesis:")))
	d.add(li(8, 0, "First"), li(8, 0, "Second"))
	d.add(p(pStyle("Heading2"), t("Multilevel numbering")))
	d.add(li(5, 0, "Chapter"), li(5, 1, "Section"), li(5, 2, "Subsection"), li(5, 1, "Section"), li(5, 0, "Chapter"))
	d.add(p(pStyle("Heading2"), t("Start and continue")))
	d.add(p("", t("A list that starts at 5:")))
	d.add(li(6, 0, "Five"), li(6, 0, "Six"))
	d.add(p("", t("The first list continues after this paragraph:")))
	d.add(li(2, 0, "Four (continued)"), li(2, 0, "Five (continued)"))
	d.add(p("", t("A new list restarts at 1:")))
	d.add(li(7, 0, "One again"), li(7, 0, "Two again"))
	d.add(p(pStyle("Heading2"), t("Long items")))
	d.add(li(1, 0, "A bullet with a long text. "+lorem))
	d.add(p(pStyle("ListParagraph"), t("A second paragraph inside the same item, without a bullet.")))
	d.add(p(pStyle("ListParagraph")+numPr(1, 0), t("Formatted: "), r("<w:b/>", "bold"), t(" and "), r(`<w:color w:val="FF0000"/>`, "red"), t(" in a bullet.")))
	d.add(p(numPr(1, 0)+`<w:ind w:left="1440" w:hanging="360"/>`, t("A bullet with a direct indent of one inch.")))
}

func tablesDoc(d *docx) {
	d.add(p(pStyle("Heading1"), t("Tables")))
	grid := []int{3000, 3000, 3000}
	cell := func(s string) string { return tc(3000, "", p("", t(s))) }
	head := func(s string) string {
		return tc(3000, `<w:shd w:val="clear" w:color="auto" w:fill="D9D9D9"/>`, p("", r("<w:b/>", s)))
	}
	d.add(p(pStyle("Heading2"), t("A simple table")))
	d.add(tbl(`<w:tblStyle w:val="TableGrid"/>`, grid,
		tr(`<w:tblHeader/>`, head("Name"), head("Quantity"), head("Price")),
		tr("", cell("Apples"), tc(3000, "", p(`<w:jc w:val="right"/>`, t("12"))), tc(3000, "", p(`<w:jc w:val="right"/>`, t("€ 3.60")))),
		tr("", cell("Pears"), tc(3000, "", p(`<w:jc w:val="right"/>`, t("7"))), tc(3000, "", p(`<w:jc w:val="right"/>`, t("€ 2.10")))),
		tr("", cell("Total"), tc(3000, "", p(`<w:jc w:val="center"/>`, t("centred"))), tc(3000, "", p(`<w:jc w:val="right"/>`, r("<w:b/>", "€ 5.70"))))))
	d.add(p(pStyle("Heading2"), t("Widths, merged cells and colours")))
	d.add(tbl(`<w:tblStyle w:val="TableGrid"/>`, []int{1500, 4500, 3000},
		tr("", tc(1500, "", p("", t("narrow"))), tc(4500, "", p("", t("wide"))), tc(3000, "", p("", t("medium")))),
		tr("", tc(6000, `<w:gridSpan w:val="2"/>`, p(`<w:jc w:val="center"/>`, t("Two columns merged"))), tc(3000, `<w:vMerge w:val="restart"/><w:vAlign w:val="center"/>`, p("", t("Two rows merged")))),
		tr("", tc(1500, `<w:shd w:val="clear" w:color="auto" w:fill="FFFF00"/>`, p("", t("yellow"))),
			tc(4500, `<w:shd w:val="clear" w:color="auto" w:fill="C6E0B4"/>`, p("", r(`<w:color w:val="C00000"/>`, "red text on green"))),
			tc(3000, `<w:vMerge/>`, p("")))))
	d.add(p(pStyle("Heading2"), t("Rich content in cells")))
	d.add(tbl(`<w:tblStyle w:val="TableGrid"/>`, []int{4500, 4500},
		tr("", tc(4500, "", p("", t("Two paragraphs:")), p("", r("<w:i/>", "the second one in italic."))),
			tc(4500, "", p(pStyle("ListParagraph")+numPr(1, 0), t("a bullet")), p(pStyle("ListParagraph")+numPr(1, 0), t("in a cell")))),
		tr(`<w:trHeight w:val="1000"/>`, tc(4500, `<w:vAlign w:val="bottom"/>`, p("", t("Bottom aligned in a tall row"))),
			tc(4500, "", p("", t("Nested table:")),
				tbl(`<w:tblStyle w:val="TableGrid"/>`, []int{2000, 2000},
					tr("", tc(2000, "", p("", t("inner 1"))), tc(2000, "", p("", t("inner 2"))))),
				p("")))))
	d.add(p(pStyle("Heading2"), t("A table without borders")))
	d.add(tbl(`<w:tblBorders><w:top w:val="nil"/><w:left w:val="nil"/><w:bottom w:val="nil"/><w:right w:val="nil"/><w:insideH w:val="nil"/><w:insideV w:val="nil"/></w:tblBorders>`, grid,
		tr("", cell("no"), cell("borders"), cell("here")),
		tr("", cell("used"), cell("for"), cell("layout"))))
	d.add(p(pStyle("Heading2"), t("A wide table")))
	var cells []string
	var heads []string
	var wgrid []int
	for i := 1; i <= 8; i++ {
		wgrid = append(wgrid, 1125)
		heads = append(heads, tc(1125, "", p("", r("<w:b/>", fmt.Sprintf("Col %d", i)))))
		cells = append(cells, tc(1125, "", p("", t(fmt.Sprintf("%d", i*i)))))
	}
	d.add(tbl(`<w:tblStyle w:val="TableGrid"/><w:jc w:val="center"/>`, wgrid, tr("", heads...), tr("", cells...)))
	d.add(p("", t("Text after the tables.")))
}

func linksImagesDoc(d *docx) {
	d.add(p(pStyle("Heading1"), t("Links and pictures")))
	ext := d.rel("hyperlink", "https://example.com/", true)
	mail := d.rel("hyperlink", "mailto:someone@example.com", true)
	query := d.rel("hyperlink", "https://example.com/search?q=word%20pad&amp;lang=en#top", true)
	link := func(id, text string) string {
		return `<w:hyperlink r:id="` + id + `" w:history="1">` + r(`<w:rStyle w:val="Hyperlink"/>`, text) + `</w:hyperlink>`
	}
	d.add(p("", t("An external link to "), link(ext, "example.com"), t(", a "), link(mail, "mail link"),
		t(" and a link with a query: "), link(query, "search"), t(".")))
	d.add(p("", t("A link made with a field: "),
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> HYPERLINK "https://www.example.org/field" </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r>`,
		r(`<w:rStyle w:val="Hyperlink"/>`, "field link"), `<w:r><w:fldChar w:fldCharType="end"/></w:r>`, t(".")))
	d.add(p("", t("A link to a bookmark: "), `<w:hyperlink w:anchor="target">`+r(`<w:rStyle w:val="Hyperlink"/>`, "go to the target")+`</w:hyperlink>`, t(".")))
	d.add(p("", t("A link with "), `<w:hyperlink r:id="`+ext+`">`+r(`<w:rStyle w:val="Hyperlink"/><w:b/>`, "bold ")+r(`<w:rStyle w:val="Hyperlink"/><w:i/>`, "italic")+`</w:hyperlink>`, t(" parts, and a bare URL: https://example.net/.")))
	d.add(p(pStyle("Heading2"), t("Pictures")))
	for _, img := range []struct {
		name string
		c    color.RGBA
		enc  func(image.Image) []byte
	}{
		{"png", color.RGBA{0x00, 0x70, 0xC0, 255}, encPNG},
		{"jpeg", color.RGBA{0xC0, 0x30, 0x00, 255}, encJPEG},
		{"gif", color.RGBA{0x00, 0xA0, 0x50, 255}, encGIF},
		{"bmp", color.RGBA{0x80, 0x30, 0xA0, 255}, encBMP},
	} {
		data := img.enc(testImage(img.c))
		d.add(p("", t(strings.ToUpper(img.name)+": "), d.image("picture-"+img.name+"."+img.name, data, 96, 64)))
	}
	png := encPNG(testImage(color.RGBA{0x40, 0x40, 0x40, 255}))
	d.add(p("", t("The PNG at half and double size: "), d.image("small.png", png, 48, 32), t(" "), d.image("large.png", png, 192, 128)))
	d.add(p(`<w:jc w:val="center"/>`, d.image("centered.png", png, 96, 64)))
	d.add(p("", t("Text before"), d.image("inline.png", png, 24, 16), t("text after a picture in the line.")))
	for i := 0; i < 20; i++ {
		d.add(p("", t("Filler paragraph so that the bookmark is further down.")))
	}
	d.add(p("", `<w:bookmarkStart w:id="0" w:name="target"/>`, t("This is the bookmark target."), `<w:bookmarkEnd w:id="0"/>`))
}

func stylesDoc(d *docx) {
	d.add(p(pStyle("Title"), t("Title style")))
	d.add(p(pStyle("Subtitle"), t("Subtitle style")))
	for i := 1; i <= 6; i++ {
		d.add(p(pStyle(fmt.Sprintf("Heading%d", i)), t(fmt.Sprintf("Heading %d", i))))
		d.add(p("", t(fmt.Sprintf("Normal text under heading %d.", i))))
	}
	d.add(p(pStyle("Quote"), t("Quote style: a quotation in italic, indented.")))
	d.add(p(pStyle("IntenseQuote"), t("Intense quote style.")))
	d.add(p(pStyle("Code"), t("func main() {")))
	d.add(p(pStyle("Code"), t("\tfmt.Println(\"code style, Courier New\")")))
	d.add(p(pStyle("Code"), t("}")))
	d.add(p(pStyle("Warning"), t("A custom style: Warning, red and bold, based on Normal.")))
	d.add(p(pStyle("Heading2"), r(`<w:color w:val="00B050"/><w:i/>`, "Heading 2 with direct green italic")))
	d.add(p(pStyle("Heading1")+`<w:jc w:val="center"/>`, t("Centred heading 1")))
	d.add(p("", t("Character styles: "), r(`<w:rStyle w:val="Strong"/>`, "Strong"), t(", "),
		r(`<w:rStyle w:val="Emphasis"/>`, "Emphasis"), t(", "), r(`<w:rStyle w:val="InlineCode"/>`, "InlineCode"), t(".")))
	d.add(p(pStyle("NoSuchStyle"), t("A paragraph with a style that is not defined.")))
}

func extrasDoc(d *docx) {
	d.header = p(`<w:jc w:val="center"/>`, t("Header text"))
	d.footer = p(`<w:jc w:val="center"/>`, t("Page "), field("PAGE", "1"), t(" of "), field("NUMPAGES", "2"))
	d.add(p(pStyle("Heading1"), t("Parts word4win leaves out")))
	d.add(p("", t("This document has a header and a footer with page numbers.")))
	d.add(p("", t("A footnote follows this word"), `<w:r><w:rPr><w:rStyle w:val="FootnoteReference"/></w:rPr><w:footnoteReference w:id="1"/></w:r>`,
		t(" and an endnote this one"), `<w:r><w:rPr><w:rStyle w:val="FootnoteReference"/></w:rPr><w:endnoteReference w:id="1"/></w:r>`, t(".")))
	d.footnote = "The footnote text."
	d.endnote = "The endnote text."
	d.add(p("", t("A "), `<w:commentRangeStart w:id="0"/>`, t("commented"), `<w:commentRangeEnd w:id="0"/>`,
		`<w:r><w:rPr><w:rStyle w:val="CommentReference"/></w:rPr><w:commentReference w:id="0"/></w:r>`, t(" word.")))
	d.comment = "The comment text."
	d.add(p("", t("Tracked changes: "),
		`<w:ins w:id="1" w:author="Reviewer" w:date="2026-11-23T10:00:00Z">`+t("inserted text")+`</w:ins>`, t(" and "),
		`<w:del w:id="2" w:author="Reviewer" w:date="2026-11-23T10:00:00Z"><w:r><w:delText>deleted text</w:delText></w:r></w:del>`,
		t("; after this colon is a deletion:"), `<w:del w:id="3" w:author="Reviewer" w:date="2026-11-23T10:00:00Z"><w:r><w:delText> GONE</w:delText></w:r></w:del>`, t(".")))
	d.add(p("", t("Fields: date "), field(`DATE \@ "yyyy-MM-dd"`, "2026-11-23"), t(", author "), field("AUTHOR", "Someone"), t(".")))
	d.add(p("", t("A content control: "), `<w:sdt><w:sdtPr><w:alias w:val="Box"/></w:sdtPr><w:sdtContent>`+r("<w:b/>", "inside the control")+`</w:sdtContent></w:sdt>`, t(".")))
	d.add(p(`<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="auto"/></w:pBdr>`, t("A paragraph with a bottom border, as Word draws a horizontal line.")))
	d.add(p("", t("A text box follows, its text may be lost:"),
		`<w:r><w:pict><v:shape xmlns:v="urn:schemas-microsoft-com:vml" style="width:200pt;height:30pt" type="#_x0000_t202"><v:textbox><w:txbxContent><w:p><w:r><w:t>text box content</w:t></w:r></w:p></w:txbxContent></v:textbox></v:shape></w:pict></w:r>`))
	d.add(p(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>`,
		t("The section ends here: the next section is landscape.")))
	d.add(p("", t("This text is in a landscape section.")))
	d.landscape = true
}

func field(instr, result string) string {
	return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> ` + esc(instr) +
		` </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r>` + t(result) + `<w:r><w:fldChar w:fldCharType="end"/></w:r>`
}

// --- WordprocessingML ---

const (
	nsW     = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	nsD     = nsW + ` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`
	relNS   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	ctPrefx = "application/vnd.openxmlformats-officedocument.wordprocessingml."
	xmlHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
)

var esc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace

func p(ppr string, content ...string) string {
	if ppr != "" {
		ppr = "<w:pPr>" + ppr + "</w:pPr>"
	}
	return "<w:p>" + ppr + strings.Join(content, "") + "</w:p>"
}

func r(rpr, text string) string {
	if rpr != "" {
		rpr = "<w:rPr>" + rpr + "</w:rPr>"
	}
	return "<w:r>" + rpr + `<w:t xml:space="preserve">` + esc(text) + "</w:t></w:r>"
}

func t(text string) string    { return r("", text) }
func pStyle(id string) string { return `<w:pStyle w:val="` + id + `"/>` }
func font(f string) string {
	return `<w:rFonts w:ascii="` + f + `" w:hAnsi="` + f + `" w:cs="` + f + `"/>`
}
func numPr(n, lvl int) string {
	return fmt.Sprintf(`<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, lvl, n)
}

func tbl(tblpr string, grid []int, rows ...string) string {
	var g strings.Builder
	for _, w := range grid {
		fmt.Fprintf(&g, `<w:gridCol w:w="%d"/>`, w)
	}
	return `<w:tbl><w:tblPr>` + tblpr + `<w:tblW w:w="0" w:type="auto"/><w:tblLook w:val="04A0"/></w:tblPr><w:tblGrid>` +
		g.String() + `</w:tblGrid>` + strings.Join(rows, "") + `</w:tbl>`
}

func tr(trpr string, cells ...string) string {
	if trpr != "" {
		trpr = "<w:trPr>" + trpr + "</w:trPr>"
	}
	return "<w:tr>" + trpr + strings.Join(cells, "") + "</w:tr>"
}

func tc(width int, tcpr string, content ...string) string {
	return fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>%s</w:tcPr>%s</w:tc>`, width, tcpr, strings.Join(content, ""))
}

type relation struct{ id, typ, target, mode string }

type docx struct {
	body                       strings.Builder
	rels                       []relation
	media                      map[string][]byte
	header, footer             string
	footnote, endnote, comment string
	landscape                  bool
}

func newDocx() *docx { return &docx{media: map[string][]byte{}} }

func (d *docx) add(xml ...string) {
	for _, x := range xml {
		d.body.WriteString(x)
	}
}

func (d *docx) rel(typ, target string, external bool) string {
	id := fmt.Sprintf("rId%d", len(d.rels)+1)
	mode := ""
	if external {
		mode = ` TargetMode="External"`
	}
	d.rels = append(d.rels, relation{id, typ, target, mode})
	return id
}

func (d *docx) image(name string, data []byte, w, h int) string {
	id := d.rel("image", "media/"+name, false)
	d.media[name] = data
	n := len(d.rels)
	cx, cy := w*9525, h*9525
	return fmt.Sprintf(`<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="%s"/>`+
		`<wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:pic><pic:nvPicPr><pic:cNvPr id="%d" name="%s"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, n, name, n, name, id, cx, cy)
}

func (d *docx) bytes() []byte {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	put := func(name, content string) {
		w, err := z.Create(name)
		if err != nil {
			log.Fatal(err)
		}
		w.Write([]byte(content))
	}
	overrides := map[string]string{
		"/word/document.xml":  "document.main+xml",
		"/word/styles.xml":    "styles+xml",
		"/word/numbering.xml": "numbering+xml",
		"/word/settings.xml":  "settings+xml",
	}
	d.rel("styles", "styles.xml", false)
	d.rel("numbering", "numbering.xml", false)
	d.rel("settings", "settings.xml", false)
	var hdr string
	if d.header != "" {
		id := d.rel("header", "header1.xml", false)
		hdr += `<w:headerReference w:type="default" r:id="` + id + `"/>`
		put("word/header1.xml", xmlHead+`<w:hdr `+nsW+`>`+d.header+`</w:hdr>`)
		overrides["/word/header1.xml"] = "header+xml"
	}
	if d.footer != "" {
		id := d.rel("footer", "footer1.xml", false)
		hdr += `<w:footerReference w:type="default" r:id="` + id + `"/>`
		put("word/footer1.xml", xmlHead+`<w:ftr `+nsW+`>`+d.footer+`</w:ftr>`)
		overrides["/word/footer1.xml"] = "footer+xml"
	}
	notes := func(kind, text string) string {
		return xmlHead + `<w:` + kind + `s ` + nsW + `>` +
			`<w:` + kind + ` w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:` + kind + `>` +
			`<w:` + kind + ` w:type="continuationSeparator" w:id="0"><w:p><w:r><w:continuationSeparator/></w:r></w:p></w:` + kind + `>` +
			`<w:` + kind + ` w:id="1"><w:p><w:pPr><w:pStyle w:val="FootnoteText"/></w:pPr><w:r><w:rPr><w:rStyle w:val="FootnoteReference"/></w:rPr><w:` + kind + `Ref/></w:r>` +
			t(" "+text) + `</w:p></w:` + kind + `></w:` + kind + `s>`
	}
	settingsNotes := ""
	if d.footnote != "" {
		d.rel("footnotes", "footnotes.xml", false)
		put("word/footnotes.xml", notes("footnote", d.footnote))
		overrides["/word/footnotes.xml"] = "footnotes+xml"
		settingsNotes += `<w:footnotePr><w:footnote w:id="-1"/><w:footnote w:id="0"/></w:footnotePr>`
	}
	if d.endnote != "" {
		d.rel("endnotes", "endnotes.xml", false)
		put("word/endnotes.xml", notes("endnote", d.endnote))
		overrides["/word/endnotes.xml"] = "endnotes+xml"
		settingsNotes += `<w:endnotePr><w:endnote w:id="-1"/><w:endnote w:id="0"/></w:endnotePr>`
	}
	if d.comment != "" {
		d.rel("comments", "comments.xml", false)
		put("word/comments.xml", xmlHead+`<w:comments `+nsW+`><w:comment w:id="0" w:author="Reviewer" w:date="2026-11-23T10:00:00Z" w:initials="R"><w:p>`+
			t(d.comment)+`</w:p></w:comment></w:comments>`)
		overrides["/word/comments.xml"] = "comments+xml"
	}
	pg := `<w:pgSz w:w="11906" w:h="16838"/>`
	if d.landscape {
		pg = `<w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/>`
	}
	sect := `<w:sectPr>` + hdr + pg + `<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>`
	put("word/document.xml", xmlHead+`<w:document `+nsD+`><w:body>`+d.body.String()+sect+`</w:body></w:document>`)
	put("word/styles.xml", stylesXML)
	put("word/numbering.xml", numberingXML())
	put("word/settings.xml", xmlHead+`<w:settings `+nsW+`><w:defaultTabStop w:val="720"/>`+settingsNotes+`<w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`)
	var rels strings.Builder
	for _, rl := range d.rels {
		fmt.Fprintf(&rels, `<Relationship Id="%s" Type="%s%s" Target="%s"%s/>`, rl.id, relNS, rl.typ, rl.target, rl.mode)
	}
	put("word/_rels/document.xml.rels", xmlHead+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+rels.String()+`</Relationships>`)
	for _, name := range sortedKeys(d.media) {
		data := d.media[name]
		w, err := z.Create("word/media/" + name)
		if err != nil {
			log.Fatal(err)
		}
		w.Write(data)
	}
	put("_rels/.rels", xmlHead+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="`+relNS+`officeDocument" Target="word/document.xml"/></Relationships>`)
	var ct strings.Builder
	ct.WriteString(xmlHead + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
		`<Default Extension="png" ContentType="image/png"/><Default Extension="jpeg" ContentType="image/jpeg"/><Default Extension="gif" ContentType="image/gif"/><Default Extension="bmp" ContentType="image/bmp"/>`)
	for _, part := range sortedKeys(overrides) {
		typ := overrides[part]
		fmt.Fprintf(&ct, `<Override PartName="%s" ContentType="%s%s"/>`, part, ctPrefx, typ)
	}
	ct.WriteString(`</Types>`)
	put("[Content_Types].xml", ct.String())
	if err := z.Close(); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

func paraStyle(id, name, based, ppr, rpr string) string {
	s := `<w:style w:type="paragraph" w:styleId="` + id + `"><w:name w:val="` + name + `"/>`
	if based != "" {
		s += `<w:basedOn w:val="` + based + `"/><w:next w:val="Normal"/>`
	}
	s += `<w:qFormat/>`
	if ppr != "" {
		s += `<w:pPr>` + ppr + `</w:pPr>`
	}
	if rpr != "" {
		s += `<w:rPr>` + rpr + `</w:rPr>`
	}
	return s + `</w:style>`
}

func charStyle(id, name, rpr string) string {
	return `<w:style w:type="character" w:styleId="` + id + `"><w:name w:val="` + name + `"/><w:rPr>` + rpr + `</w:rPr></w:style>`
}

var stylesXML = func() string {
	var s strings.Builder
	s.WriteString(xmlHead + `<w:styles ` + nsW + `><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:eastAsia="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/>` +
		`<w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="en-US" w:eastAsia="en-US" w:bidi="ar-SA"/></w:rPr></w:rPrDefault>` +
		`<w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="259" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>`)
	s.WriteString(`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>`)
	s.WriteString(`<w:style w:type="character" w:default="1" w:styleId="DefaultParagraphFont"><w:name w:val="Default Paragraph Font"/><w:uiPriority w:val="1"/><w:semiHidden/></w:style>`)
	sizes := []int{32, 26, 24, 22, 22, 22}
	for i := 1; i <= 6; i++ {
		rpr := font("Calibri Light") + `<w:color w:val="2F5496"/>` + fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, sizes[i-1], sizes[i-1])
		if i <= 3 {
			rpr += `<w:b/>`
		} else {
			rpr += `<w:i/>`
		}
		s.WriteString(paraStyle(fmt.Sprintf("Heading%d", i), fmt.Sprintf("heading %d", i), "Normal",
			fmt.Sprintf(`<w:keepNext/><w:keepLines/><w:spacing w:before="240" w:after="60"/><w:outlineLvl w:val="%d"/>`, i-1), rpr))
	}
	s.WriteString(paraStyle("Title", "Title", "Normal", `<w:spacing w:after="0" w:line="240" w:lineRule="auto"/><w:contextualSpacing/>`,
		font("Calibri Light")+`<w:spacing w:val="-10"/><w:kern w:val="28"/><w:sz w:val="56"/><w:szCs w:val="56"/>`))
	s.WriteString(paraStyle("Subtitle", "Subtitle", "Normal", ``, `<w:color w:val="5A5A5A"/><w:spacing w:val="15"/>`))
	s.WriteString(paraStyle("Quote", "Quote", "Normal", `<w:spacing w:before="200"/><w:ind w:left="864" w:right="864"/><w:jc w:val="center"/>`, `<w:i/><w:color w:val="404040"/>`))
	s.WriteString(paraStyle("IntenseQuote", "Intense Quote", "Normal",
		`<w:pBdr><w:top w:val="single" w:sz="4" w:space="10" w:color="4472C4"/><w:bottom w:val="single" w:sz="4" w:space="10" w:color="4472C4"/></w:pBdr><w:spacing w:before="360" w:after="360"/><w:ind w:left="864" w:right="864"/><w:jc w:val="center"/>`,
		`<w:i/><w:color w:val="4472C4"/>`))
	s.WriteString(paraStyle("ListParagraph", "List Paragraph", "Normal", `<w:ind w:left="720"/><w:contextualSpacing/>`, ``))
	s.WriteString(paraStyle("Code", "Code", "Normal", `<w:spacing w:after="0" w:line="240" w:lineRule="auto"/><w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/>`,
		font("Courier New")+`<w:sz w:val="20"/>`))
	s.WriteString(paraStyle("Warning", "Warning", "Normal", ``, `<w:b/><w:color w:val="C00000"/>`))
	s.WriteString(paraStyle("FootnoteText", "footnote text", "Normal", `<w:spacing w:after="0" w:line="240" w:lineRule="auto"/>`, `<w:sz w:val="20"/>`))
	s.WriteString(charStyle("FootnoteReference", "footnote reference", `<w:vertAlign w:val="superscript"/>`))
	s.WriteString(charStyle("CommentReference", "annotation reference", `<w:sz w:val="16"/>`))
	s.WriteString(charStyle("Hyperlink", "Hyperlink", `<w:color w:val="0563C1"/><w:u w:val="single"/>`))
	s.WriteString(charStyle("Strong", "Strong", `<w:b/>`))
	s.WriteString(charStyle("Emphasis", "Emphasis", `<w:i/>`))
	s.WriteString(charStyle("InlineCode", "Inline Code", font("Consolas")+`<w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/>`))
	s.WriteString(`<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr>` +
		`<w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:left w:val="single" w:sz="4" w:space="0" w:color="auto"/>` +
		`<w:bottom w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:right w:val="single" w:sz="4" w:space="0" w:color="auto"/>` +
		`<w:insideH w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="auto"/></w:tblBorders>` +
		`<w:tblCellMar><w:left w:w="108" w:type="dxa"/><w:right w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>`)
	s.WriteString(`</w:styles>`)
	return s.String()
}()

func numberingXML() string {
	lvl := func(i int, format, text, font string) string {
		rpr := ""
		if font != "" {
			rpr = `<w:rPr><w:rFonts w:ascii="` + font + `" w:hAnsi="` + font + `" w:hint="default"/></w:rPr>`
		}
		return fmt.Sprintf(`<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%s"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr>%s</w:lvl>`,
			i, format, esc(text), 720*(i+1), rpr)
	}
	abstract := func(id int, levels ...string) string {
		return fmt.Sprintf(`<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="hybridMultilevel"/>%s</w:abstractNum>`, id, strings.Join(levels, ""))
	}
	var s strings.Builder
	s.WriteString(xmlHead + `<w:numbering ` + nsW + `>`)
	s.WriteString(abstract(0, lvl(0, "bullet", "", "Symbol"), lvl(1, "bullet", "o", "Courier New"), lvl(2, "bullet", "", "Wingdings")))
	s.WriteString(abstract(1, lvl(0, "decimal", "%1.", ""), lvl(1, "lowerLetter", "%2.", ""), lvl(2, "lowerRoman", "%3.", "")))
	s.WriteString(abstract(2, lvl(0, "upperLetter", "%1.", "")))
	s.WriteString(abstract(3, lvl(0, "upperRoman", "%1.", "")))
	s.WriteString(abstract(4, lvl(0, "decimal", "%1.", ""), lvl(1, "decimal", "%1.%2.", ""), lvl(2, "decimal", "%1.%2.%3.", "")))
	s.WriteString(abstract(5, lvl(0, "decimal", "%1)", "")))
	for i, a := range []int{0, 1, 2, 3, 4, 1, 1, 5} {
		over := ""
		switch i + 1 {
		case 6:
			over = `<w:lvlOverride w:ilvl="0"><w:startOverride w:val="5"/></w:lvlOverride>`
		case 7:
			over = `<w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride>`
		}
		fmt.Fprintf(&s, `<w:num w:numId="%d"><w:abstractNumId w:val="%d"/>%s</w:num>`, i+1, a, over)
	}
	s.WriteString(`</w:numbering>`)
	return s.String()
}

// --- pictures ---

// testImage is a 96x64 picture: a colour with a white diagonal and a black
// frame, so that scaling and cropping show.
func testImage(c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 96, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 96; x++ {
			px := c
			switch {
			case x < 2 || y < 2 || x >= 94 || y >= 62:
				px = color.RGBA{0, 0, 0, 255}
			case abs(x*2-y*3) < 4:
				px = color.RGBA{255, 255, 255, 255}
			}
			img.Set(x, y, px)
		}
	}
	return img
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func encPNG(img image.Image) []byte {
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func encJPEG(img image.Image) []byte {
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	return b.Bytes()
}

func encGIF(img image.Image) []byte {
	var b bytes.Buffer
	gif.Encode(&b, img, nil)
	return b.Bytes()
}

// encBMP writes a 24-bit bottom-up BMP.
func encBMP(img image.Image) []byte {
	bnd := img.Bounds()
	w, h := bnd.Dx(), bnd.Dy()
	stride := (w*3 + 3) &^ 3
	size := 54 + stride*h
	b := make([]byte, 54, size)
	copy(b, "BM")
	binary.LittleEndian.PutUint32(b[2:], uint32(size))
	binary.LittleEndian.PutUint32(b[10:], 54)
	binary.LittleEndian.PutUint32(b[14:], 40)
	binary.LittleEndian.PutUint32(b[18:], uint32(w))
	binary.LittleEndian.PutUint32(b[22:], uint32(h))
	binary.LittleEndian.PutUint16(b[26:], 1)
	binary.LittleEndian.PutUint16(b[28:], 24)
	binary.LittleEndian.PutUint32(b[34:], uint32(stride*h))
	binary.LittleEndian.PutUint32(b[38:], 3780)
	binary.LittleEndian.PutUint32(b[42:], 3780)
	for y := h - 1; y >= 0; y-- {
		row := make([]byte, stride)
		for x := 0; x < w; x++ {
			cr, cg, cb, _ := img.At(bnd.Min.X+x, bnd.Min.Y+y).RGBA()
			row[x*3], row[x*3+1], row[x*3+2] = byte(cb>>8), byte(cg>>8), byte(cr>>8)
		}
		b = append(b, row...)
	}
	return b
}

// writeMarkdownHTML writes the Markdown and HTML sources, with the picture
// they link next to them.
func writeMarkdownHTML() {
	pic := encPNG(testImage(color.RGBA{0x00, 0x70, 0xC0, 255}))
	files := map[string]string{"markdown.md": markdownSrc, "html.html": htmlSrc(pic)}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0o644); err != nil {
			log.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outDir, "picture.png"), pic, 0o644); err != nil {
		log.Fatal(err)
	}
}

const markdownSrc = `# Markdown reference

Plain, **bold**, *italic*, ***bold italic***, ~~strikethrough~~, ` + "`inline code`" + ` and a
[link](https://example.com/ "with a title"). A bare link: https://example.org/.
Escaped \*stars\* and a backslash \\ and HTML entities &amp; &copy;.

A hard line break follows  
this line, and a soft break
joins this one.

## Lists

- one
- two
  - nested
    - deeper
- three

1. first
2. second
   1. sub one
   2. sub two
3. third

5. starts at five
6. six

- [ ] task to do
- [x] task done

## Quotes and code

> A quote
> over two lines.
>
> > A nested quote.

` + "```go" + `
func main() {
	fmt.Println("hi")
}
` + "```" + `

    indented code block

## Table

| Left | Center | Right |
| :--- | :----: | ----: |
| a    | b      | 1     |
| **c** | *d*   | 22    |

## Other

A picture: ![picture](picture.png)

---

### Heading 3

#### Heading 4

##### Heading 5

###### Heading 6

Unicode: café, Straße, € “quotes” — αβγ 漢字 😀.
`

func htmlSrc(pic []byte) string {
	data := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pic)
	return `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>HTML reference</title></head>
<body>
<h1>HTML reference</h1>
<p>Plain, <b>bold</b>, <strong>strong</strong>, <i>italic</i>, <em>em</em>, <u>underline</u>, <s>strike</s>,
<code>code</code>, x<sup>2</sup>, H<sub>2</sub>O and a <a href="https://example.com/">link</a>.</p>
<p>Deleted text: <del>del</del>.</p>
<p>Line one<br>line two after a br.</p>
<h2>Fonts and colours</h2>
<p><span style="font-family: 'Times New Roman', serif">Times New Roman</span>,
<span style="font-family: Arial, sans-serif">Arial</span>,
<span style="font-family: monospace">monospace</span>,
<font face="Georgia" size="5">font tag size 5</font>,
<span style="font-size: 8pt">8pt</span>, <span style="font-size: 24px">24px</span>, <span style="font-size: 2em">2em</span>.</p>
<p><span style="color: red">red</span>, <span style="color: #00B050">#00B050</span>, <span style="color: rgb(0, 112, 192)">rgb()</span>,
<font color="purple">font color</font>, <span style="background-color: yellow">yellow background</span>,
<span style="background: cyan">cyan background</span>,
<span style="font-weight: bold; font-style: italic; text-decoration: underline line-through">all styles in CSS</span>.</p>
<p>Raised in CSS: <span style="vertical-align: super">super</span> and <span style="vertical-align: sub">sub</span>.</p>
<h2>Paragraphs</h2>
<p style="text-align: left">Left aligned.</p>
<p style="text-align: center">Centred.</p>
<p align="right">Right aligned with the align attribute.</p>
<p style="text-align: justify">Justified. The editor wraps this paragraph over several lines so that the alignment is visible: the quick brown fox jumps over the lazy dog, and the lazy dog does not seem to mind at all.</p>
<p style="margin-left: 40px">Indented 40px.</p>
<div>A div instead of a p.</div>
<blockquote>A blockquote.</blockquote>
<pre>preformatted
	with a tab
    and spaces</pre>
<h2>Lists</h2>
<ul><li>one</li><li>two<ul><li>nested</li></ul></li></ul>
<ol><li>first</li><li>second<ol type="a"><li>letter</li></ol></li></ol>
<ol start="5" style="list-style-type: upper-roman"><li>five in Roman</li><li>six</li></ol>
<ol type="A"><li>upper letter</li></ol>
<h2>Table</h2>
<table border="1">
<tr><th>Name</th><th style="text-align: right">Value</th></tr>
<tr><td>a</td><td style="text-align: right">1</td></tr>
<tr><td colspan="2" style="text-align: center">two columns</td></tr>
</table>
<h2>Pictures</h2>
<p>Embedded: <img src="` + data + `" alt="embedded" width="96" height="64">, linked: <img src="picture.png" alt="linked">.</p>
<p>Scaled by its width only: <img src="picture.png" width="48"></p>
<hr>
<h3>Heading 3</h3><h4>Heading 4</h4><h5>Heading 5</h5><h6>Heading 6</h6>
<p>Entities: &amp; &lt; &gt; &nbsp;non-breaking &euro; &copy; &#8212; &#x1F600;. Unicode: café, αβγ, 漢字.</p>
</body>
</html>
`
}
