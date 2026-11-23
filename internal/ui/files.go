//go:build windows

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mevdschee/word4win/internal/doc"
	"github.com/mevdschee/word4win/internal/layout"
	"github.com/mevdschee/word4win/internal/pdf"
	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
	"github.com/rodrigocfd/windigo/x/cosh"
	"github.com/rodrigocfd/windigo/x/winsh"
)

const emEmptyUndoBuffer = 0x00CD

var openTypes = []winsh.COMDLG_FILTERSPEC{
	{Name: "All documents", Spec: "*.rtf;*.md;*.markdown;*.html;*.htm;*.docx;*.odt;*.txt"},
	{Name: "Rich Text Format (*.rtf)", Spec: "*.rtf"},
	{Name: "Markdown (*.md)", Spec: "*.md;*.markdown"},
	{Name: "HTML (*.html)", Spec: "*.html;*.htm"},
	{Name: "Word document (*.docx)", Spec: "*.docx"},
	{Name: "OpenDocument Text (*.odt)", Spec: "*.odt"},
	{Name: "Text documents (*.txt)", Spec: "*.txt"},
	{Name: "All files (*.*)", Spec: "*.*"},
}

// The formats that can be saved, in the order of the Save As list.
var saveFormats = []struct {
	format doc.Format
	spec   winsh.COMDLG_FILTERSPEC
	ext    string
}{
	{doc.FormatRTF, winsh.COMDLG_FILTERSPEC{Name: "Rich Text Format (*.rtf)", Spec: "*.rtf"}, "rtf"},
	{doc.FormatMarkdown, winsh.COMDLG_FILTERSPEC{Name: "Markdown (*.md)", Spec: "*.md;*.markdown"}, "md"},
	{doc.FormatHTML, winsh.COMDLG_FILTERSPEC{Name: "HTML (*.html)", Spec: "*.html;*.htm"}, "html"},
	{doc.FormatText, winsh.COMDLG_FILTERSPEC{Name: "Text document (*.txt)", Spec: "*.txt"}, "txt"},
}

func startIn(rel *win.OleReleaser, dlg *winsh.IFileDialog, current string) {
	if current == "" {
		return
	}
	var folder *winsh.IShellItem
	if winsh.SHCreateItemFromParsingName(rel, filepath.Dir(current), &folder) == nil {
		dlg.SetFolder(folder)
	}
}

func dialogResult(rel *win.OleReleaser, dlg *winsh.IFileDialog, owner win.HWND) (string, bool) {
	if ok, _ := dlg.Show(owner); !ok {
		return "", false
	}
	item, err := dlg.GetResult(rel)
	if err != nil {
		return "", false
	}
	path, err := item.GetDisplayName(cosh.SIGDN_FILESYSPATH)
	return path, err == nil && path != ""
}

func openFileDialog(owner win.HWND, current string, types []winsh.COMDLG_FILTERSPEC) (string, bool) {
	rel := win.NewOleReleaser()
	defer rel.Release()
	var dlg *winsh.IFileOpenDialog
	if err := win.CoCreateInstance(rel, &cosh.CLSID_FileOpenDialog, nil, co.CLSCTX_INPROC_SERVER, &dlg); err != nil {
		return "", false
	}
	opts, _ := dlg.GetOptions()
	dlg.SetOptions(opts | cosh.FOS_FORCEFILESYSTEM | cosh.FOS_FILEMUSTEXIST)
	dlg.SetFileTypes(types)
	startIn(rel, &dlg.IFileDialog, current)
	return dialogResult(rel, &dlg.IFileDialog, owner)
}

// saveFileDialog asks for a file name for types; it returns the path and
// the index of the chosen type.
func saveFileDialog(owner win.HWND, current, name string, types []winsh.COMDLG_FILTERSPEC, exts []string, index int) (string, int, bool) {
	rel := win.NewOleReleaser()
	defer rel.Release()
	var dlg *winsh.IFileSaveDialog
	if err := win.CoCreateInstance(rel, &cosh.CLSID_FileSaveDialog, nil, co.CLSCTX_INPROC_SERVER, &dlg); err != nil {
		return "", 0, false
	}
	opts, _ := dlg.GetOptions()
	dlg.SetOptions(opts | cosh.FOS_FORCEFILESYSTEM | cosh.FOS_OVERWRITEPROMPT)
	dlg.SetFileTypes(types)
	dlg.SetFileTypeIndex(index + 1)
	dlg.SetDefaultExtension(exts[index])
	dlg.SetFileName(name)
	startIn(rel, &dlg.IFileDialog, current)
	path, ok := dialogResult(rel, &dlg.IFileDialog, owner)
	if !ok {
		return "", 0, false
	}
	chosen := index
	if i, err := dlg.GetFileTypeIndex(); err == nil && i >= 1 && i <= len(types) {
		chosen = i - 1
	}
	// The extension of the name wins over the chosen type.
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	for i, e := range exts {
		if ext == e || ext == "markdown" && e == "md" || ext == "htm" && e == "html" {
			chosen = i
		}
	}
	return path, chosen, true
}

// newDocument empties the editor, with the default font.
func (a *App) newDocument() {
	a.re.SetRTF(doc.WriteRTF(&doc.Document{Blocks: []doc.Block{&doc.Paragraph{SpaceAfter: 6}}}), false)
	cf := newCharFormat()
	cf.Mask = cfmFace | cfmSize | cfmColor | cfmCharset
	cf.setFace(doc.DefaultFont)
	cf.Height = int32(doc.DefaultSize * 20)
	cf.Effects = cfeAutoColor
	cf.CharSet = 0
	a.re.SetDefaultCharFormat(&cf)
	a.re.send(emEmptyUndoBuffer, 0, 0)
	a.re.SetModified(false)
	a.path, a.format, a.warned = "", "", false
	a.updateTitle()
	a.updateFormat()
	a.countWords()
}

// confirmSave asks to save changes; it returns false to cancel.
func (a *App) confirmSave() bool {
	if a.re == nil || !a.re.Modified() {
		return true
	}
	ret, _ := a.wnd.Hwnd().MessageBox(fmt.Sprintf("Do you want to save changes to %s?", a.name()), appName,
		co.MB_YESNOCANCEL|co.MB_ICONEXCLAMATION)
	switch ret {
	case co.ID_YES:
		return a.save()
	case co.ID_NO:
		return true
	}
	return false
}

func (a *App) openDialog() {
	if !a.confirmSave() {
		return
	}
	if path, ok := openFileDialog(a.wnd.Hwnd(), a.path, openTypes); ok {
		a.open(path)
	}
}

// open reads a file into the editor.
func (a *App) open(path string) bool {
	restore := waitCursor()
	data, err := os.ReadFile(path)
	if err != nil {
		restore()
		a.fileError("open", path, err)
		return false
	}
	format := doc.FormatOf(path, data)
	var rtf []byte
	if format == doc.FormatRTF {
		rtf = data
	} else {
		d, _, err := doc.ReadFile(path, data)
		if err != nil {
			restore()
			a.fileError("open", path, err)
			return false
		}
		rtf = doc.WriteRTF(d)
	}
	a.re.SetRTF(rtf, false)
	a.re.send(emEmptyUndoBuffer, 0, 0)
	a.re.SetSelection(0, 0)
	a.re.SetModified(false)
	restore()
	a.path, a.format, a.warned = path, format, false
	a.cfg.AddRecent(path)
	a.updateTitle()
	a.updateFormat()
	a.countWords()
	return true
}

// start handles the command line.
func (a *App) start(opts Options) {
	if opts.Path == "" {
		return
	}
	path := opts.Path
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if _, err := os.Stat(path); os.IsNotExist(err) && !opts.Print {
		ret, _ := a.wnd.Hwnd().MessageBox(fmt.Sprintf("Cannot find %s.\n\nDo you want to create a new file?", path),
			appName, co.MB_YESNO|co.MB_ICONEXCLAMATION)
		if ret == co.ID_YES {
			a.path = path
			a.format = doc.FormatOf(path, nil)
			a.updateTitle()
		}
		return
	}
	if a.open(path) && opts.Print {
		a.printNow()
		a.wnd.Hwnd().PostMessage(co.WM_CLOSE, 0, 0)
	}
}

// savable tells whether a format can be written.
func savable(f doc.Format) bool {
	return f == doc.FormatRTF || f == doc.FormatMarkdown || f == doc.FormatHTML || f == doc.FormatText
}

func (a *App) save() bool {
	if a.path == "" || !savable(a.format) {
		return a.saveAs("")
	}
	return a.write(a.path, a.format)
}

// saveAs asks for a name; format preselects a type ("" keeps the current).
func (a *App) saveAs(format doc.Format) bool {
	if format == "" {
		format = a.format
	}
	index := 0
	types := make([]winsh.COMDLG_FILTERSPEC, len(saveFormats))
	exts := make([]string, len(saveFormats))
	for i, f := range saveFormats {
		types[i], exts[i] = f.spec, f.ext
		if f.format == format {
			index = i
		}
	}
	name := strings.TrimSuffix(a.name(), filepath.Ext(a.name()))
	path, chosen, ok := saveFileDialog(a.wnd.Hwnd(), a.path, name, types, exts, index)
	if !ok {
		return false
	}
	f := saveFormats[chosen].format
	if f != a.format {
		a.warned = false
	}
	if !a.write(path, f) {
		return false
	}
	a.path, a.format = path, f
	a.cfg.AddRecent(path)
	a.updateTitle()
	return true
}

// model converts the editor's content to the document model.
func (a *App) model() *doc.Document {
	d, err := doc.ReadRTF(a.re.RTF(false))
	if err != nil {
		return doc.FromPlainText(a.re.Text())
	}
	return d
}

// write saves the document in a format, after warning about what the
// format cannot keep.
func (a *App) write(path string, f doc.Format) bool {
	var data []byte
	var err error
	switch f {
	case doc.FormatRTF:
		data = a.re.RTF(false)
	case doc.FormatMarkdown:
		d := a.model()
		if !a.warned && !doc.IsMarkdownSafe(d) {
			ret, _ := a.wnd.Hwnd().MessageBox("Markdown keeps headings, bold, italic, lists, tables, links and pictures. "+
				"Fonts, colors, sizes and alignment are lost.\n\nSave as Markdown anyway?", appName, co.MB_YESNO|co.MB_ICONEXCLAMATION)
			if ret != co.ID_YES {
				return false
			}
		}
		a.warned = true
		restore := waitCursor()
		data, err = doc.WriteMarkdown(d, doc.FileImageSaver(path))
		restore()
	case doc.FormatHTML:
		restore := waitCursor()
		data, err = doc.WriteHTML(a.model(), strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), doc.DataImageSaver)
		restore()
	case doc.FormatText:
		if !a.warned {
			ret, _ := a.wnd.Hwnd().MessageBox("You are about to save the document in a text-only format, which will remove all formatting. "+
				"Are you sure you want to do this?", appName, co.MB_YESNO|co.MB_ICONEXCLAMATION)
			if ret != co.ID_YES {
				return false
			}
		}
		a.warned = true
		text := strings.ReplaceAll(a.re.Text(), "\r\n", "\r")
		text = strings.ReplaceAll(text, "\r", "\r\n")
		data = []byte(text)
	default:
		return false
	}
	if err == nil {
		err = writeFileAtomic(path, data)
	}
	if err != nil {
		a.fileError("save", path, err)
		return false
	}
	a.re.SetModified(false)
	a.updateTitle()
	return true
}

// writeFileAtomic writes a new file next to the old one and swaps them.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".w4w-tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return os.WriteFile(path, data, 0o644)
	}
	return nil
}

// layoutSettings are the page setup for print, preview and PDF.
func (a *App) layoutSettings() layout.Settings {
	return layout.Settings{
		PageWidth: a.cfg.PaperWidth, PageHeight: a.cfg.PaperHeight,
		MarginLeft: a.cfg.MarginLeft, MarginTop: a.cfg.MarginTop,
		MarginRight: a.cfg.MarginRight, MarginBottom: a.cfg.MarginBottom,
		Scale: float64(a.cfg.PrintScale) / 100,
	}
}

func (a *App) exportPDF() {
	name := strings.TrimSuffix(a.name(), filepath.Ext(a.name()))
	path, _, ok := saveFileDialog(a.wnd.Hwnd(), a.path, name,
		[]winsh.COMDLG_FILTERSPEC{{Name: "PDF document (*.pdf)", Spec: "*.pdf"}}, []string{"pdf"}, 0)
	if !ok {
		return
	}
	restore := waitCursor()
	pages := layout.Layout(a.model(), a.layoutSettings())
	data, err := pdf.Write(pages, name)
	if err == nil {
		err = writeFileAtomic(path, data)
	}
	restore()
	if err != nil {
		a.fileError("export", path, err)
		return
	}
	ret, _ := a.wnd.Hwnd().MessageBox(fmt.Sprintf("%s is saved, %d pages.\n\nOpen it now?", filepath.Base(path), len(pages)),
		appName, co.MB_YESNO|co.MB_ICONINFORMATION)
	if ret == co.ID_YES {
		shellOpen(a.wnd.Hwnd(), path)
	}
}

// ---- inserting ----

func (a *App) insertRuns(runs []doc.Run) {
	a.re.SetRTF(doc.InlineRTF(runs), true)
	a.re.Hwnd().SetFocus()
}

func (a *App) insertPicture() {
	path, ok := openFileDialog(a.wnd.Hwnd(), a.path, []winsh.COMDLG_FILTERSPEC{
		{Name: "Pictures", Spec: "*.png;*.jpg;*.jpeg;*.gif;*.bmp"},
		{Name: "All files (*.*)", Spec: "*.*"},
	})
	if !ok {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.fileError("open", path, err)
		return
	}
	img, err := doc.LoadImage(data)
	if err != nil {
		a.wnd.Hwnd().MessageBox("This picture cannot be inserted: "+err.Error(), appName, co.MB_ICONEXCLAMATION)
		return
	}
	// No wider than the text on the page.
	maxW := a.cfg.PaperWidth - a.cfg.MarginLeft - a.cfg.MarginRight
	if img.Width > maxW {
		img.Height, img.Width = img.Height*maxW/img.Width, maxW
	}
	img.Alt = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	a.insertRuns([]doc.Run{{Image: img}})
}

func (a *App) insertTable(rows, cols int) {
	if rows < 1 || cols < 1 {
		return
	}
	t := &doc.Table{}
	w := (a.cfg.PaperWidth - a.cfg.MarginLeft - a.cfg.MarginRight) / float64(cols)
	for c := 0; c < cols; c++ {
		t.ColWidths = append(t.ColWidths, w)
	}
	for r := 0; r < rows; r++ {
		row := doc.Row{}
		for c := 0; c < cols; c++ {
			row.Cells = append(row.Cells, doc.Cell{Blocks: []doc.Block{&doc.Paragraph{}}})
		}
		t.Rows = append(t.Rows, row)
	}
	rtf := doc.WriteRTF(&doc.Document{Blocks: []doc.Block{t}})
	a.re.SetRTF(rtf, true)
	a.re.Hwnd().SetFocus()
}

func (a *App) insertRule() {
	rtf := doc.WriteRTF(&doc.Document{Blocks: []doc.Block{&doc.Paragraph{Rule: true, SpaceAfter: 6}}})
	a.re.SetRTF(rtf, true)
	a.re.Hwnd().SetFocus()
}
