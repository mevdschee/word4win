package doc

import (
	"os"
	"strings"
	"testing"
)

// The DOCX and ODT files in testdata were made by LibreOffice from the RTF
// that WriteRTF makes of testdata/sample.md.
func TestReadPackages(t *testing.T) {
	for _, name := range []string{"testdata/sample.docx", "testdata/sample.odt"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		d, f, err := ReadFile(name, data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if f != FormatDOCX && f != FormatODT {
			t.Errorf("%s: format %q", name, f)
		}
		md, err := WriteMarkdown(d, DataImageSaver)
		if err != nil {
			t.Fatal(err)
		}
		got := string(md)
		for _, want := range []string{
			"# Title\n", "**bold**", "*italic*", "~~gone~~", "`code`", "](https://example.com",
			"- one\n", "  - nested\n", "- [x] done\n", "1. first\n", "> A quote\n",
			"```\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```\n", "\n---\n",
			"| Name | Value |\n| --- | ---: |\n", "![](data:image/png;base64,", "   1. sub a\n",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: missing %q in\n%s", name, want, got)
			}
		}
	}
}

func TestMarkdownImage(t *testing.T) {
	data, _ := os.ReadFile("testdata/sample.md")
	d := ReadMarkdown(data, "testdata")
	found := false
	for _, b := range d.Blocks {
		if p, ok := b.(*Paragraph); ok && hasImage(p) {
			found = true
			if img := p.Runs[0].Image; img.Width != 36 || img.Format != "png" {
				t.Errorf("image %+v", img.Width)
			}
		}
	}
	if !found {
		t.Error("no image")
	}
}

// testdata/wine.rtf is what Wine's RichEdit writes of testdata/sample.md:
// swapped \li and \fi, hyphens for the rule and a NUL at the end.
func TestReadWineRTF(t *testing.T) {
	data, _ := os.ReadFile("testdata/wine.rtf")
	d, err := ReadRTF(data)
	if err != nil {
		t.Fatal(err)
	}
	md, _ := WriteMarkdown(d, DataImageSaver)
	got := string(md)
	for _, want := range []string{"  - nested\n", "> A quote\n", "\n---\n", "   2. sub b\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.HasSuffix(strings.TrimSpace(got), "3.") {
		t.Errorf("an empty list item at the end:\n%s", got)
	}
}
