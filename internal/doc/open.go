package doc

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// Format is a file format.
type Format string

const (
	FormatRTF      Format = "rtf"
	FormatMarkdown Format = "md"
	FormatText     Format = "txt"
	FormatDOCX     Format = "docx"
	FormatODT      Format = "odt"
	FormatPDF      Format = "pdf"
	FormatHTML     Format = "html"
)

// FormatOf tells the format of a file from its name and content. Content
// wins over the extension for RTF, DOCX and ODT.
func FormatOf(path string, data []byte) Format {
	switch Detect(data) {
	case "rtf":
		return FormatRTF
	case "docx":
		return FormatDOCX
	case "odt":
		return FormatODT
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown", ".mkd", ".mkdn":
		return FormatMarkdown
	case ".html", ".htm", ".xhtml":
		return FormatHTML
	case ".docx", ".docm", ".dotx", ".odt", ".ott", ".doc":
		return "" // not readable: the content did not match
	}
	return FormatText
}

// ReadFile converts the content of a file to a document.
func ReadFile(path string, data []byte) (*Document, Format, error) {
	f := FormatOf(path, data)
	var d *Document
	var err error
	switch f {
	case FormatRTF:
		d, err = ReadRTF(data)
	case FormatDOCX:
		d, err = ReadDOCX(data)
	case FormatODT:
		d, err = ReadODT(data)
	case FormatMarkdown:
		d = ReadMarkdown([]byte(DecodeText(data)), filepath.Dir(path))
	case FormatHTML:
		d = ReadHTML(data, filepath.Dir(path))
	case FormatText:
		d = FromPlainText(DecodeText(data))
		for _, b := range d.Blocks {
			p := b.(*Paragraph)
			for i := range p.Runs {
				p.Runs[i].Font, p.Runs[i].Size = CodeFont, DefaultSize
			}
		}
	default:
		if strings.EqualFold(filepath.Ext(path), ".doc") {
			return nil, f, fmt.Errorf("Word 97-2003 documents (.doc) cannot be read; save it as .docx in Word or LibreOffice")
		}
		return nil, f, fmt.Errorf("the file is not in a format that can be read")
	}
	return d, f, err
}

// DecodeText decodes text in UTF-8 (with or without BOM), UTF-16 with a
// BOM, or Windows-1252.
func DecodeText(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return string(data[3:])
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}), bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		be := data[0] == 0xFE
		u := make([]uint16, 0, len(data)/2)
		for i := 2; i+1 < len(data); i += 2 {
			if be {
				u = append(u, uint16(data[i])<<8|uint16(data[i+1]))
			} else {
				u = append(u, uint16(data[i+1])<<8|uint16(data[i]))
			}
		}
		return string(utf16.Decode(u))
	case utf8.Valid(data):
		return string(data)
	}
	out, err := charmap.Windows1252.NewDecoder().Bytes(data)
	if err != nil {
		return string(data)
	}
	return string(out)
}
