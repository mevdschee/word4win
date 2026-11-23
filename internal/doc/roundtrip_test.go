package doc

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRoundTrip opens every source in testdata/writer the way the app does
// and saves it back to its own format: the document goes into the editor as
// RTF (WriteRTF), comes out of it as RTF (ReadRTF; the RichEdit control in
// between is not available in a test) and is written in the format it came
// from. Reading that file again must give the document that was opened.
// DOCX and ODT cannot be saved, so they are saved as RTF, like the app does.
func TestRoundTrip(t *testing.T) {
	var sources []string
	for _, ext := range []string{"docx", "odt", "rtf", "txt", "md", "html"} {
		files, _ := filepath.Glob("testdata/writer/*." + ext)
		for _, f := range files {
			if !strings.HasSuffix(f, ".writer.rtf") {
				sources = append(sources, f)
			}
		}
	}
	if len(sources) == 0 {
		t.Fatal("no sources in testdata/writer: run go run ./tools/genrefdocs")
	}
	for _, src := range sources {
		t.Run(filepath.Base(src), func(t *testing.T) {
			opened := readForCompare(t, src)
			edited, err := ReadRTF(WriteRTF(opened))
			if err != nil {
				t.Fatal(err)
			}
			ext := strings.ToLower(filepath.Ext(src))
			var saved []byte
			switch ext {
			case ".md":
				saved, err = WriteMarkdown(edited, DataImageSaver)
			case ".html":
				saved, err = WriteHTML(edited, "", DataImageSaver)
			case ".txt":
				saved = []byte(strings.ReplaceAll(edited.PlainText(), "\n", "\r\n"))
			default:
				ext = ".rtf"
				saved = WriteRTF(edited)
			}
			if err != nil {
				t.Fatal(err)
			}
			again, _, err := ReadFile("saved"+ext, saved)
			if err != nil {
				t.Fatal(err)
			}
			if g, w := again.PlainText(), opened.PlainText(); g != w {
				t.Errorf("text differs after saving as %s:\n%s", ext, firstDiffNamed(g, w, "saved", "opened"))
			}
			if ext == ".txt" {
				return
			}
			gh, _ := WriteHTML(again, "", DataImageSaver)
			wh, _ := WriteHTML(opened, "", DataImageSaver)
			if string(gh) != string(wh) {
				dump(t, "saved", gh)
				dump(t, "opened", wh)
				t.Errorf("formatting differs after saving as %s:\n%s", ext, firstDiffNamed(string(gh), string(wh), "saved", "opened"))
			}
		})
	}
}
