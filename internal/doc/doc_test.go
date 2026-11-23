package doc

import (
	"strings"
	"testing"
)

const sampleMD = `# Title

Some **bold**, *italic*, ~~gone~~ and ` + "`code`" + ` text with a [link](https://example.com).

## Lists

- one
- two
  - nested
- [x] done

1. first
2. second

> A quote

` + "```" + `
func main() {
	fmt.Println("hi")
}
` + "```" + `

---

| Name | Value |
| --- | ---: |
| a | 1 |
| b | 2 |
`

func TestMarkdownRoundTripThroughRTF(t *testing.T) {
	d := ReadMarkdown([]byte(sampleMD), "")
	rtf := WriteRTF(d)
	d2, err := ReadRTF(rtf)
	if err != nil {
		t.Fatal(err)
	}
	md, err := WriteMarkdown(d2, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := string(md)
	for _, want := range []string{
		"# Title\n", "**bold**", "*italic*", "~~gone~~", "`code`", "[link](https://example.com)",
		"## Lists\n", "- one\n", "  - nested\n", "- [x] done\n", "1. first\n", "2. second\n",
		"> A quote\n", "```\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n", "---\n",
		"| Name | Value |\n| --- | ---: |\n| a | 1 |\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s\n--- rtf:\n%s", want, got, rtf)
		}
	}
}

func TestMarkdownDirect(t *testing.T) {
	d := ReadMarkdown([]byte(sampleMD), "")
	md, _ := WriteMarkdown(d, nil)
	d2 := ReadMarkdown(md, "")
	md2, _ := WriteMarkdown(d2, nil)
	if string(md) != string(md2) {
		t.Errorf("not stable:\n%s\n---\n%s", md, md2)
	}
}

func TestRTFReadWordPad(t *testing.T) {
	src := `{\rtf1\ansi\ansicpg1252\deff0\nouicompat\deflang1033{\fonttbl{\f0\fnil\fcharset0 Calibri;}{\f1\fnil\fcharset2 Symbol;}}
{\colortbl ;\red255\green0\blue0;}
{\*\generator Riched20 10.0.19041}\viewkind4\uc1 
\pard\sa200\sl276\slmult1\qc\b\f0\fs22\lang9 Centered bold\b0\par
\pard\sa200\sl276\slmult1 Caf\'e9 \cf1 red\cf0  \u8364?\par
\pard{\pntext\f1\'B7\tab}{\*\pn\pnlvlblt\pnf1\pnindent0{\pntxtb\'B7}}\fi-360\li720\sa200\sl276\slmult1 bullet one\par
{\pntext\f1\'B7\tab}bullet two\par
\pard\sa200\sl276\slmult1\par
}`
	d, err := ReadRTF([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Blocks) != 5 {
		t.Fatalf("got %d blocks: %#v", len(d.Blocks), d.Blocks)
	}
	p0 := d.Blocks[0].(*Paragraph)
	if p0.Align != AlignCenter || !p0.Runs[0].Bold || p0.Text() != "Centered bold" {
		t.Errorf("first paragraph: %+v", p0)
	}
	p1 := d.Blocks[1].(*Paragraph)
	if p1.Text() != "Café red €" {
		t.Errorf("text %q", p1.Text())
	}
	if len(p1.Runs) < 2 || p1.Runs[1].Color == nil || *p1.Runs[1].Color != (Color{255, 0, 0}) {
		t.Errorf("colour: %+v", p1.Runs)
		return
	}
	p2 := d.Blocks[2].(*Paragraph)
	p3 := d.Blocks[3].(*Paragraph)
	if p2.List == nil || p2.List.Ordered || p3.List == nil || p3.Text() != "bullet two" || p2.List.Level != 0 {
		t.Errorf("bullets: %+v %+v", p2, p3)
	}
}

func TestRTFTableAndLink(t *testing.T) {
	d := &Document{Blocks: []Block{
		&Table{Rows: []Row{
			{Cells: []Cell{{Blocks: []Block{&Paragraph{Runs: []Run{{Text: "a"}}}}}, {Blocks: []Block{&Paragraph{Runs: []Run{{Text: "b"}}}}}}},
			{Cells: []Cell{{Blocks: []Block{&Paragraph{Runs: []Run{{Text: "c"}}}}}, {Blocks: []Block{&Paragraph{Runs: []Run{{Text: "d", Link: "http://x.y/"}}}}}}},
		}},
		&Paragraph{Runs: []Run{{Text: "after"}}},
	}}
	d2, _ := ReadRTF(WriteRTF(d))
	if len(d2.Blocks) != 2 {
		t.Fatalf("blocks: %d", len(d2.Blocks))
	}
	tbl, ok := d2.Blocks[0].(*Table)
	if !ok || len(tbl.Rows) != 2 || len(tbl.Rows[1].Cells) != 2 {
		t.Fatalf("table: %#v", d2.Blocks[0])
	}
	p := tbl.Rows[1].Cells[1].Blocks[0].(*Paragraph)
	if p.Text() != "d" || p.Runs[0].Link != "http://x.y/" {
		t.Errorf("cell: %+v", p)
	}
}

func TestHTMLRoundTrip(t *testing.T) {
	d := ReadMarkdown([]byte(sampleMD), "")
	page, err := WriteHTML(d, "Sample", nil)
	if err != nil {
		t.Fatal(err)
	}
	d2 := ReadHTML(page, "")
	md, _ := WriteMarkdown(d2, nil)
	got := string(md)
	for _, want := range []string{
		"# Title\n", "**bold**", "*italic*", "~~gone~~", "`code`", "[link](https://example.com)",
		"- one\n", "  - nested\n", "- [x] done\n", "1. first\n", "2. second\n", "> A quote\n",
		"```\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n", "---\n", "| Name | Value |\n| --- | ---: |\n| a | 1 |\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s\n--- html:\n%s", want, got, page)
		}
	}
}

func TestHTMLStyles(t *testing.T) {
	src := `<html><body><p style="text-align:center">Hello <span style="color:#ff0000;font-weight:bold">red</span>
	<font face="Arial" size="5">big</font></p><ol start="3"><li>c</li><li>d</li></ol></body></html>`
	d := ReadHTML([]byte(src), "")
	p := d.Blocks[0].(*Paragraph)
	if p.Align != AlignCenter || p.Text() != "Hello red big" {
		t.Fatalf("paragraph %+v %q", p, p.Text())
	}
	if r := p.Runs[1]; !r.Bold || r.Color == nil || *r.Color != (Color{255, 0, 0}) {
		t.Errorf("run %+v", r)
	}
	if r := p.Runs[3]; r.Font != "Arial" || r.Size != 18 {
		t.Errorf("font run %+v", r)
	}
	if l := d.Blocks[1].(*Paragraph).List; l == nil || l.Start != 3 {
		t.Errorf("list %+v", l)
	}
}
