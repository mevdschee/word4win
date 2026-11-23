// Command word4win-cli converts documents with the converters of word4win,
// without the window. It reads RTF, Markdown, HTML, DOCX, ODT and text,
// and writes RTF, Markdown, HTML, text and PDF:
//
//	word4win-cli in.docx out.pdf
//	word4win-cli -scale 80 -paper a4 in.md out.pdf
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mevdschee/word4win/internal/doc"
)

func main() {
	scale := flag.Int("scale", 100, "PDF: scale in percent")
	paper := flag.String("paper", "a4", "PDF: a4 or letter")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: word4win-cli [-scale N] [-paper a4|letter] in.(rtf|md|html|docx|odt|txt) out.(rtf|md|html|txt|pdf)")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	if err := convert(flag.Arg(0), flag.Arg(1), *scale, *paper); err != nil {
		fmt.Fprintln(os.Stderr, "word4win-cli:", err)
		os.Exit(1)
	}
}

func convert(in, out string, scale int, paper string) error {
	data, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	d, _, err := doc.ReadFile(in, data)
	if err != nil {
		return err
	}
	var result []byte
	switch strings.ToLower(filepath.Ext(out)) {
	case ".rtf":
		result = doc.WriteRTF(d)
	case ".md", ".markdown":
		result, err = doc.WriteMarkdown(d, doc.FileImageSaver(out))
	case ".html", ".htm":
		result, err = doc.WriteHTML(d, strings.TrimSuffix(filepath.Base(in), filepath.Ext(in)), doc.FileImageSaver(out))
	case ".txt":
		result = []byte(d.PlainText())
	case ".pdf":
		result, err = writePDF(d, scale, paper)
	default:
		return fmt.Errorf("cannot write %s", filepath.Ext(out))
	}
	if err != nil {
		return err
	}
	return os.WriteFile(out, result, 0o644)
}
