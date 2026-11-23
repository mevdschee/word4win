package doc

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// node is an XML element with local names (the namespace prefixes are
// dropped: DOCX and ODT do not reuse local names in a way that matters
// here, except where the namespace is checked).
type node struct {
	name     string
	space    string
	attrs    []xml.Attr
	children []*node
	text     string // character data directly in this element, for leaf elements
	// Mixed content keeps the order of text and elements: a text child is
	// a node with name "#text".
}

func (n *node) attr(local string) string {
	for _, a := range n.attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func (n *node) hasAttr(local string) bool {
	for _, a := range n.attrs {
		if a.Name.Local == local {
			return true
		}
	}
	return false
}

func (n *node) child(local string) *node {
	if n == nil {
		return nil
	}
	for _, c := range n.children {
		if c.name == local {
			return c
		}
	}
	return nil
}

func (n *node) all(local string) []*node {
	if n == nil {
		return nil
	}
	var out []*node
	for _, c := range n.children {
		if c.name == local {
			out = append(out, c)
		}
	}
	return out
}

// find returns the first descendant with a local name, depth first.
func (n *node) find(local string) *node {
	if n == nil {
		return nil
	}
	for _, c := range n.children {
		if c.name == local {
			return c
		}
		if f := c.find(local); f != nil {
			return f
		}
	}
	return nil
}

// textContent is all character data below a node.
func (n *node) textContent() string {
	if n == nil {
		return ""
	}
	if n.name == "#text" {
		return n.text
	}
	var b strings.Builder
	for _, c := range n.children {
		b.WriteString(c.textContent())
	}
	return b.String()
}

func parseXML(data []byte) (*node, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	root := &node{name: "#document"}
	stack := []*node{root}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{name: t.Name.Local, space: t.Name.Space, attrs: append([]xml.Attr{}, t.Attr...)}
			top := stack[len(stack)-1]
			top.children = append(top.children, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			top := stack[len(stack)-1]
			top.children = append(top.children, &node{name: "#text", text: string(t)})
		}
	}
	if len(root.children) == 0 {
		return nil, fmt.Errorf("empty XML document")
	}
	for _, c := range root.children {
		if c.name != "#text" {
			return c, nil
		}
	}
	return nil, fmt.Errorf("no XML root element")
}

// zipFiles reads a zip archive into memory, by name.
type zipFiles map[string]*zip.File

func openZip(data []byte) (zipFiles, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := zipFiles{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	return files, nil
}

// read returns the content of a file in the archive; nil when it is
// missing. Files over 256 MB are refused.
func (z zipFiles) read(name string) []byte {
	f, ok := z[name]
	if !ok {
		// Names are case-insensitive in practice.
		for n, ff := range z {
			if strings.EqualFold(n, name) {
				f, ok = ff, true
				break
			}
		}
		if !ok {
			return nil
		}
	}
	if f.UncompressedSize64 > 256<<20 {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, 256<<20))
	if err != nil {
		return nil
	}
	return data
}

func (z zipFiles) xml(name string) *node {
	data := z.read(name)
	if data == nil {
		return nil
	}
	n, err := parseXML(data)
	if err != nil {
		return nil
	}
	return n
}

// parseLength reads a CSS-like length ("2cm", "0.5in", "12pt", "10mm",
// "1pc", "96px") in points.
func parseLength(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		factor float64
	}{{"pt", 1}, {"cm", 72 / 2.54}, {"mm", 72 / 25.4}, {"in", 72}, {"pc", 12}, {"px", 0.75}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, u.suffix)), 64)
			if err != nil {
				return 0, false
			}
			return v * u.factor, true
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

func parseHexColor(s string) (*Color, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return nil, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return nil, false
	}
	return &Color{uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
}

func atoiDefault(s string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return v
}

// mediaFormat tells the picture format of a file, by content.
func mediaFormat(data []byte) string {
	switch {
	case len(data) > 8 && bytes.HasPrefix(data, []byte("\x89PNG")):
		return "png"
	case len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8:
		return "jpeg"
	}
	return ""
}

// imageFromMedia makes an image from a picture file in a package,
// converting GIF and BMP to PNG. Other formats (EMF, WMF, SVG) are skipped.
func imageFromMedia(data []byte, width, height float64) *Image {
	if data == nil {
		return nil
	}
	var img *Image
	if f := mediaFormat(data); f != "" {
		img = &Image{Data: data, Format: f}
	} else if li, err := LoadImage(data); err == nil {
		img = li
	} else {
		return nil
	}
	if width > 0 && height > 0 {
		img.Width, img.Height = width, height
	} else if li, err := LoadImage(img.Data); err == nil {
		img.Width, img.Height = li.Width, li.Height
	}
	return img
}

// Detect tells the format of a file by its content: "rtf", "docx", "odt",
// or "" when it is not one of these.
func Detect(data []byte) string {
	trimmed := bytes.TrimLeft(data, "\xef\xbb\xbf \r\n\t")
	if bytes.HasPrefix(trimmed, []byte(`{\rtf`)) {
		return "rtf"
	}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		z, err := openZip(data)
		if err != nil {
			return ""
		}
		if mt := z.read("mimetype"); mt != nil {
			if strings.HasPrefix(string(mt), "application/vnd.oasis.opendocument.text") {
				return "odt"
			}
		}
		if _, ok := z["word/document.xml"]; ok {
			return "docx"
		}
		if _, ok := z["content.xml"]; ok {
			return "odt"
		}
	}
	return ""
}
