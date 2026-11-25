//go:build windows

package ui

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/mevdschee/word4win/internal/doc"
	"github.com/mevdschee/word4win/internal/update"
	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"
)

func send(h win.HWND, msg co.WM, w, l uintptr) uintptr {
	r, _ := h.SendMessage(msg, win.WPARAM(w), win.LPARAM(l))
	return r
}

// ---- Find and Replace (the common dialogs, as WordPad has) ----

type findReplace struct {
	size        uint32
	owner       uintptr
	instance    uintptr
	flags       uint32
	findWhat    *uint16
	replaceWith *uint16
	findLen     uint16
	replaceLen  uint16
	custData    uintptr
	hook        uintptr
	template    *uint16
}

type findState struct {
	dlg     win.HWND
	fr      findReplace
	what    [256]uint16
	with    [256]uint16
	replace bool
}

const (
	frFindNext   = 0x0008
	frReplace    = 0x0010
	frReplaceAll = 0x0020
	frTerm       = 0x0040
	frNoUpDown   = 0x0400
)

var findMsgID co.WM

func findMsg() co.WM {
	if findMsgID == 0 {
		r, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(utf16Ptr("commdlg_FindReplace"))))
		findMsgID = co.WM(r)
	}
	return findMsgID
}

func (a *App) showFind(replace bool) {
	f := &a.find
	if f.dlg != 0 {
		if f.replace == replace {
			f.dlg.SetFocus()
			return
		}
		a.closeFind()
	}
	// Start with the selected word.
	if s, e := a.re.Selection(); e > s && e-s < 200 {
		sel := a.re.TextRange(s, e)
		if !strings.ContainsAny(sel, "\r\n") {
			u, _ := syscall.UTF16FromString(sel)
			copy(f.what[:len(f.what)-1], u)
		}
	}
	f.replace = replace
	f.fr = findReplace{owner: uintptr(a.wnd.Hwnd()), flags: frDown, findWhat: &f.what[0], replaceWith: &f.with[0],
		findLen: uint16(len(f.what)), replaceLen: uint16(len(f.with))}
	if a.cfg.MatchCase {
		f.fr.flags |= frMatchCase
	}
	if a.cfg.WholeWord {
		f.fr.flags |= frWholeWord
	}
	f.fr.size = uint32(unsafe.Sizeof(f.fr))
	proc := procFindTextW
	if replace {
		proc = procReplaceTextW
	}
	h, _, _ := proc.Call(uintptr(unsafe.Pointer(&f.fr)))
	f.dlg = win.HWND(h)
	if f.dlg != 0 {
		themeDialog(f.dlg)
	}
}

func (a *App) closeFind() {
	if a.find.dlg != 0 {
		a.find.dlg.DestroyWindow()
		a.find.dlg = 0
	}
}

func (a *App) findMessage(lParam win.LPARAM) {
	fr := *(**findReplace)(unsafe.Pointer(&lParam))
	f := &a.find
	if fr.flags&frTerm != 0 {
		f.dlg = 0
		return
	}
	a.cfg.MatchCase = fr.flags&frMatchCase != 0
	a.cfg.WholeWord = fr.flags&frWholeWord != 0
	what := syscall.UTF16ToString(f.what[:])
	with := syscall.UTF16ToString(f.with[:])
	if what == "" {
		return
	}
	flags := fr.flags & (frDown | frMatchCase | frWholeWord)
	switch {
	case fr.flags&frReplaceAll != 0:
		a.replaceAll(what, with, flags|frDown)
	case fr.flags&frReplace != 0:
		s, e := a.re.Selection()
		if e > s && a.matches(a.re.TextRange(s, e), what, flags) {
			a.re.ReplaceSelection(with)
		}
		a.findText(what, flags|frDown, true)
	case fr.flags&frFindNext != 0:
		a.findText(what, flags, true)
	}
}

func (a *App) matches(text, what string, flags uint32) bool {
	if flags&frMatchCase != 0 {
		return text == what
	}
	return strings.EqualFold(text, what)
}

// findText selects the next match; it wraps around once.
func (a *App) findText(what string, flags uint32, report bool) bool {
	s, e := a.re.Selection()
	var ms, me int
	if flags&frDown != 0 {
		ms, me = a.re.Find(what, e, -1, flags)
		if ms < 0 {
			ms, me = a.re.Find(what, 0, -1, flags)
		}
	} else {
		ms, me = a.re.Find(what, s, 0, flags)
		if ms < 0 {
			ms, me = a.re.Find(what, a.re.TextLength(), 0, flags)
		}
	}
	if ms < 0 {
		if report {
			owner := a.wnd.Hwnd()
			if a.find.dlg != 0 {
				owner = a.find.dlg
			}
			owner.MessageBox(fmt.Sprintf("Cannot find \"%s\".", what), appName, co.MB_ICONINFORMATION)
		}
		return false
	}
	a.re.SetSelection(ms, me)
	a.re.ScrollCaret()
	return true
}

func (a *App) findNext() {
	what := syscall.UTF16ToString(a.find.what[:])
	if what == "" {
		a.showFind(false)
		return
	}
	flags := uint32(frDown)
	if a.cfg.MatchCase {
		flags |= frMatchCase
	}
	if a.cfg.WholeWord {
		flags |= frWholeWord
	}
	a.findText(what, flags, true)
}

func (a *App) replaceAll(what, with string, flags uint32) {
	count := 0
	pos := 0
	a.re.send(uint32(co.WM_SETREDRAW), 0, 0)
	for {
		ms, me := a.re.Find(what, pos, -1, flags)
		if ms < 0 {
			break
		}
		a.re.SetSelection(ms, me)
		a.re.ReplaceSelection(with)
		u, _ := syscall.UTF16FromString(with)
		pos = ms + len(u) - 1
		count++
		if count > 1000000 {
			break
		}
	}
	a.re.send(uint32(co.WM_SETREDRAW), 1, 0)
	invalidate(a.re.Hwnd())
	owner := a.wnd.Hwnd()
	if a.find.dlg != 0 {
		owner = a.find.dlg
	}
	owner.MessageBox(fmt.Sprintf("%d replacements made.", count), appName, co.MB_ICONINFORMATION)
}

// ---- Font ----

func (a *App) fontDialog() {
	cur := a.re.CharFormat()
	var lf logFont
	lf.Height = -int32(mulDiv(int(cur.Height), screenDpi(), 1440))
	lf.Weight = 400
	if cur.Effects&cfeBold != 0 {
		lf.Weight = 700
	}
	if cur.Effects&cfeItalic != 0 {
		lf.Italic = 1
	}
	if cur.Effects&cfeUnderline != 0 {
		lf.Underline = 1
	}
	if cur.Effects&cfeStrikeout != 0 {
		lf.StrikeOut = 1
	}
	lf.CharSet = cur.CharSet
	lf.setFace(cur.face())
	cf := struct {
		size      uint32
		owner     uintptr
		hdc       uintptr
		logFont   *logFont
		pointSize int32
		flags     uint32
		color     uint32
		custData  uintptr
		hook      uintptr
		template  *uint16
		instance  uintptr
		style     *uint16
		fontType  uint16
		_         uint16
		sizeMin   int32
		sizeMax   int32
	}{owner: uintptr(a.wnd.Hwnd()), logFont: &lf, color: cur.TextColor,
		flags: 0x00000001 | 0x00000040 | 0x00000100 | 0x01000000} // CF_SCREENFONTS | CF_INITTOLOGFONTSTRUCT | CF_EFFECTS | CF_NOVERTFONTS
	cf.size = uint32(unsafe.Sizeof(cf))
	if r, _, _ := procChooseFontW.Call(uintptr(unsafe.Pointer(&cf))); r == 0 {
		return
	}
	n := newCharFormat()
	n.Mask = cfmSize | cfmBold | cfmItalic | cfmUnderline | cfmStrikeout | cfmColor | cfmCharset
	n.setFace(lf.face())
	n.Height = cf.pointSize * 2 // tenths of a point to twips
	n.CharSet = lf.CharSet
	if lf.Weight >= 600 {
		n.Effects |= cfeBold
	}
	if lf.Italic != 0 {
		n.Effects |= cfeItalic
	}
	if lf.Underline != 0 {
		n.Effects |= cfeUnderline
	}
	if lf.StrikeOut != 0 {
		n.Effects |= cfeStrikeout
	}
	n.TextColor = cf.color
	a.re.SetCharFormat(&n)
	a.updateFormat()
}

var customColors [16]uint32

func chooseColor(owner win.HWND, initial uint32) (uint32, bool) {
	for i := range customColors {
		if customColors[i] == 0 {
			customColors[i] = 0xFFFFFF
		}
	}
	cc := struct {
		size     uint32
		owner    uintptr
		instance uintptr
		result   uint32
		custom   *uint32
		flags    uint32
		custData uintptr
		hook     uintptr
		template *uint16
	}{owner: uintptr(owner), result: initial, custom: &customColors[0], flags: 0x0001 | 0x0002} // CC_RGBINIT | CC_FULLOPEN
	cc.size = uint32(unsafe.Sizeof(cc))
	if r, _, _ := procChooseColorW.Call(uintptr(unsafe.Pointer(&cc))); r == 0 {
		return 0, false
	}
	return cc.result, true
}

// ---- a small form builder for the dialogs ----

type form struct {
	wnd *ui.Modal
	y   int
}

func newForm(a *App, title string, w, h int) *form {
	return &form{wnd: newModal(a.wnd, ui.OptsModal().Title(title).Size(ui.Dpi(w, h))), y: 14}
}

func (f *form) label(text string, x, y, w int) {
	ui.NewStatic(f.wnd, ui.OptsStatic().Text(text).Position(ui.Dpi(x, y+3)).Size(ui.Dpi(w, 20)))
}

func (f *form) edit(text string, x, y, w int) *ui.Edit {
	return ui.NewEdit(f.wnd, ui.OptsEdit().Text(text).Position(ui.Dpi(x, y)).Width(ui.DpiX(w)))
}

func (f *form) buttons(x, y int, ok func() bool) {
	okBtn := ui.NewButton(f.wnd, ui.OptsButton().Text("OK").Position(ui.Dpi(x, y)).Width(ui.DpiX(84)).
		CtrlStyle(co.BS_DEFPUSHBUTTON|co.BS_PUSHBUTTON).CtrlId(uint16(co.ID_OK)))
	ui.NewButton(f.wnd, ui.OptsButton().Text("Cancel").Position(ui.Dpi(x+92, y)).Width(ui.DpiX(84)).CtrlId(uint16(co.ID_CANCEL)))
	_ = okBtn
	f.wnd.On().WmCommandAccelMenu(uint16(co.ID_OK), func() {
		if ok() {
			closeModal(f.wnd)
		}
	})
	f.wnd.On().WmCommandAccelMenu(uint16(co.ID_CANCEL), func() { closeModal(f.wnd) })
}

// units: the measurement for lengths in dialogs.
func (a *App) unitName() string {
	if a.cfg.Units == "cm" {
		return "cm"
	}
	return `"`
}

func (a *App) twipsToText(tw int32) string {
	v := float64(tw) / 1440
	if a.cfg.Units == "cm" {
		v *= 2.54
	}
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64) + a.unitName()
}

func (a *App) textToTwips(s string) (int32, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	unit := a.cfg.Units
	for _, u := range []struct{ suffix, unit string }{{"cm", "cm"}, {"mm", "mm"}, {`"`, "in"}, {"in", "in"}, {"pt", "pt"}} {
		if strings.HasSuffix(s, u.suffix) {
			s, unit = strings.TrimSpace(strings.TrimSuffix(s, u.suffix)), u.unit
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.Abs(v) > 100 && unit != "pt" && unit != "mm" {
		return 0, false
	}
	switch unit {
	case "cm":
		v = v / 2.54 * 1440
	case "mm":
		v = v / 25.4 * 1440
	case "pt":
		v *= 20
	default:
		v *= 1440
	}
	return int32(math.Round(v)), true
}

// ---- Paragraph ----

func (a *App) paragraphDialog() {
	pf := a.re.ParaFormat()
	f := newForm(a, "Paragraph", 330, 300)
	f.label("Indentation", 14, 10, 200)
	f.label("Left:", 30, 36, 110)
	left := f.edit(a.twipsToText(pf.StartIndent+pf.Offset), 150, 34, 80)
	f.label("Right:", 30, 64, 110)
	right := f.edit(a.twipsToText(pf.RightIndent), 150, 62, 80)
	f.label("First line:", 30, 92, 110)
	first := f.edit(a.twipsToText(-pf.Offset), 150, 90, 80)
	f.label("Spacing", 14, 124, 200)
	f.label("Before (pt):", 30, 150, 110)
	before := f.edit(strconv.Itoa(int(pf.SpaceBefore/20)), 150, 148, 80)
	f.label("After (pt):", 30, 178, 110)
	after := f.edit(strconv.Itoa(int(pf.SpaceAfter/20)), 150, 176, 80)
	f.label("Line spacing:", 30, 206, 110)
	lines := "1"
	switch pf.LineRule {
	case 1:
		lines = "1.5"
	case 2:
		lines = "2"
	case 5:
		lines = strconv.FormatFloat(float64(pf.LineSpacing)/20, 'f', -1, 64)
	}
	spacing := f.edit(lines, 150, 204, 80)
	f.buttons(60, 250, func() bool {
		l, ok1 := a.textToTwips(windowText(left.Hwnd()))
		r, ok2 := a.textToTwips(windowText(right.Hwnd()))
		fi, ok3 := a.textToTwips(windowText(first.Hwnd()))
		b, err4 := strconv.ParseFloat(strings.TrimSpace(windowText(before.Hwnd())), 64)
		af, err5 := strconv.ParseFloat(strings.TrimSpace(windowText(after.Hwnd())), 64)
		ls, err6 := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(windowText(spacing.Hwnd())), ",", "."), 64)
		if !ok1 || !ok2 || !ok3 || err4 != nil || err5 != nil || err6 != nil || ls <= 0 || ls > 10 {
			f.wnd.Hwnd().MessageBox("Please enter valid numbers.", appName, co.MB_ICONEXCLAMATION)
			return false
		}
		n := newParaFormat()
		n.Mask = pfmStartIndent | pfmOffset | pfmRightIndent | pfmSpaceBefore | pfmSpaceAfter | pfmLineSpacing
		n.Offset = -fi
		n.StartIndent = max(0, l+fi)
		n.RightIndent = r
		n.SpaceBefore = int32(b * 20)
		n.SpaceAfter = int32(af * 20)
		if ls == 1 {
			n.LineRule = 0
		} else {
			n.LineRule = 5
			n.LineSpacing = int32(math.Round(ls * 20))
		}
		a.re.SetParaFormat(&n)
		a.updateFormat()
		return true
	})
	f.wnd.ShowModal()
}

// ---- Date and time ----

func (a *App) dateTimeDialog() {
	var formats []string
	add := func(s string) {
		if s == "" {
			return
		}
		for _, f := range formats {
			if f == s {
				return
			}
		}
		formats = append(formats, s)
	}
	add(localeFormat(procGetDateFormatW, 0x0001, ""))        // DATE_SHORTDATE
	add(localeFormat(procGetDateFormatW, 0x0002, ""))        // DATE_LONGDATE
	add(localeFormat(procGetDateFormatW, 0, "yyyy-MM-dd"))   // ISO
	add(localeFormat(procGetDateFormatW, 0, "d MMMM yyyy"))  //
	add(localeFormat(procGetDateFormatW, 0, "MMMM d, yyyy")) //
	add(localeFormat(procGetDateFormatW, 0, "dddd d MMMM yyyy"))
	add(localeFormat(procGetTimeFormatW, 0x0002, "")) // TIME_NOSECONDS
	add(localeFormat(procGetTimeFormatW, 0, ""))
	add(localeFormat(procGetDateFormatW, 0x0001, "") + " " + localeFormat(procGetTimeFormatW, 0x0002, ""))
	add(time.Now().Format("2006-01-02 15:04"))
	f := newForm(a, "Date and time", 300, 300)
	f.label("Available formats:", 14, 10, 260)
	var list win.HWND
	selected := func() int { return int(int32(send(list, co.WM(0x0188), 0, 0))) } // LB_GETCURSEL
	f.buttons(108, 250, func() bool {
		i := selected()
		if i < 0 || i >= len(formats) {
			return false
		}
		a.re.ReplaceSelection(formats[i])
		return true
	})
	f.wnd.On().WmCreate(func(_ ui.WmCreate) int {
		hInst, _ := f.wnd.Hwnd().HInstance()
		h, _, _ := procCreateWindowExW.Call(0x0200, uintptr(unsafe.Pointer(utf16Ptr("LISTBOX"))), 0,
			uintptr(co.WS_CHILD|co.WS_VISIBLE|co.WS_VSCROLL|co.WS_TABSTOP)|0x0001, // LBS_NOTIFY
			uintptr(ui.DpiX(14)), uintptr(ui.DpiY(34)), uintptr(ui.DpiX(270)), uintptr(ui.DpiY(200)),
			uintptr(f.wnd.Hwnd()), 0x7301, uintptr(hInst), 0)
		list = win.HWND(h)
		send(list, co.WM_SETFONT, uiFont(0, 0), 1)
		for _, s := range formats {
			send(list, co.WM(0x0180), 0, uintptr(unsafe.Pointer(utf16Ptr(s)))) // LB_ADDSTRING
		}
		send(list, co.WM(0x0186), 0, 0) // LB_SETCURSEL
		list.SetFocus()
		return 0
	})
	f.wnd.On().WmCommand(0x7301, co.CMD(2), func() { // LBN_DBLCLK
		if i := selected(); i >= 0 && i < len(formats) {
			a.re.ReplaceSelection(formats[i])
			closeModal(f.wnd)
		}
	})
	f.wnd.ShowModal()
}

// ---- Link ----

func (a *App) linkDialog() {
	s, e := a.re.Selection()
	text := strings.TrimSpace(a.re.TextRange(s, e))
	url := ""
	if strings.Contains(text, "://") || strings.HasPrefix(text, "www.") {
		url = text
	}
	if e > s {
		if u := a.linkURL(s, e); u != "" {
			url = u
		}
	}
	f := newForm(a, "Insert link", 420, 140)
	f.label("Text:", 14, 16, 70)
	textEdit := f.edit(text, 90, 14, 310)
	f.label("Address:", 14, 48, 70)
	urlEdit := f.edit(url, 90, 46, 310)
	f.buttons(224, 90, func() bool {
		t := strings.TrimSpace(windowText(textEdit.Hwnd()))
		u := strings.TrimSpace(windowText(urlEdit.Hwnd()))
		if u == "" {
			return false
		}
		if !strings.Contains(u, ":") && !strings.HasPrefix(u, "#") {
			if strings.Contains(u, "@") {
				u = "mailto:" + u
			} else {
				u = "https://" + u
			}
		}
		if t == "" {
			t = u
		}
		cf := a.re.CharFormat()
		run := doc.Run{Text: t, Link: u}
		if cf.Mask&cfmFace != 0 {
			run.Font = cf.face()
		}
		if cf.Mask&cfmSize != 0 {
			run.Size = float64(cf.Height) / 20
		}
		a.insertRuns([]doc.Run{run})
		return true
	})
	f.wnd.On().WmCreate(func(_ ui.WmCreate) int {
		if url == "" {
			urlEdit.Hwnd().SetFocus()
		}
		return 0
	})
	f.wnd.ShowModal()
}

// ---- About and updates ----

func showAbout(a *App) {
	wnd := newModal(a.wnd, ui.OptsModal().Title("About "+appName).Size(ui.Dpi(460, 190)))
	x, w := 124, 320
	text := func(t string, y, h int) *ui.Static {
		return ui.NewStatic(wnd, ui.OptsStatic().Text(t).Position(ui.Dpi(x, y)).Size(ui.Dpi(w, h)).
			CtrlStyle(co.SS_LEFT|co.SS_NOPREFIX))
	}
	ver := "Version " + a.version
	if !update.IsRelease(a.version) {
		ver = "Development build (" + a.version + ")"
	}
	title := text(appName, 20, 34)
	text(ver, 58, 20)
	text("A WordPad for Windows that supports Markdown.", 86, 20)
	text(fmt.Sprintf("© %d %s", max(time.Now().Year(), 2026), author), 114, 20)
	text("Released under the MIT License.", 136, 20)

	wnd.On().WmCommandAccelMenu(uint16(co.ID_OK), func() { closeModal(wnd) })
	wnd.On().WmCommandAccelMenu(uint16(co.ID_CANCEL), func() { closeModal(wnd) })

	normal := uiFont(0, 0)
	titleFont := uiFont(100, 600)
	iconSize := ui.DpiX(72)
	hInst, _ := a.wnd.Hwnd().HInstance()
	icon, _, _ := procLoadImageW.Call(uintptr(hInst), iconId, 1, uintptr(iconSize), uintptr(iconSize), 0)

	wnd.On().WmCreate(func(_ ui.WmCreate) int {
		for _, c := range wnd.Hwnd().EnumChildWindows() {
			c.SendMessage(co.WM_SETFONT, win.WPARAM(normal), 1)
		}
		title.Hwnd().SendMessage(co.WM_SETFONT, win.WPARAM(titleFont), 1)
		return 0
	})
	var linkRect win.RECT
	wnd.On().WmPaint(func() {
		var ps win.PAINTSTRUCT
		hdcW, err := wnd.Hwnd().BeginPaint(&ps)
		if err != nil {
			return
		}
		defer wnd.Hwnd().EndPaint(&ps)
		hdc := uintptr(hdcW)
		if icon != 0 {
			procDrawIconEx.Call(hdc, uintptr(ui.DpiX(26)), uintptr(ui.DpiY(24)), icon, uintptr(iconSize), uintptr(iconSize), 0, 0, 3)
		}
		old, _, _ := procSelectObject.Call(hdc, normal)
		left, top := int32(ui.DpiX(x)), int32(ui.DpiY(158))
		linkRect = win.RECT{Left: left, Top: top, Right: left + textWidth(hdc, repoURL), Bottom: top + int32(ui.DpiY(20))}
		drawText(hdc, repoURL, linkRect, dtSingleLine, colors.accent)
		procSelectObject.Call(hdc, old)
	})
	wnd.On().WmSetCursor(func(p ui.WmSetCursor) bool {
		if p.Hwnd() != wnd.Hwnd() || p.Raw.LParam.LoWord() != 1 {
			return wnd.Hwnd().DefWindowProc(co.WM_SETCURSOR, p.Raw.WParam, p.Raw.LParam) != 0
		}
		pt, err := win.GetCursorPos()
		if err == nil {
			wnd.Hwnd().ScreenToClientPt(&pt)
		}
		if err == nil && inRect(linkRect, pt) {
			procSetCursor.Call(loadCursor(32649))
		} else {
			procSetCursor.Call(loadCursor(idcArrow))
		}
		return true
	})
	wnd.On().WmLButtonUp(func(p ui.WmMouse) {
		if inRect(linkRect, p.Pos()) {
			shellOpen(wnd.Hwnd(), repoURL)
		}
	})
	wnd.ShowModal()
	if icon != 0 {
		procDestroyIcon.Call(icon)
	}
}

// installed reports whether this is the copy the MSI installed, in
// %LOCALAPPDATA%\Programs\word4win, and not the portable exe. A portable copy
// cannot update itself: the MSI would install a second copy next to it.
func installed() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "word4win")
	return strings.EqualFold(filepath.Clean(filepath.Dir(exe)), filepath.Clean(dir))
}

func (a *App) checkForUpdates() {
	h := a.wnd.Hwnd()
	if !update.IsRelease(a.version) {
		h.MessageBox("This is a development build ("+a.version+"). Updates are only for installed releases.", appName, co.MB_ICONINFORMATION)
		return
	}
	restore := waitCursor()
	r, ok, err := update.Latest(context.Background(), a.version)
	restore()
	switch {
	case err != nil:
		h.MessageBox("Could not check for updates.\n\n"+err.Error(), appName, co.MB_ICONEXCLAMATION)
		return
	case !ok || !update.Newer(r.Version, a.version):
		h.MessageBox(appName+" is up to date. You have the latest version ("+a.version+").", appName, co.MB_ICONINFORMATION)
		return
	case !installed():
		ret, _ := h.MessageBox(fmt.Sprintf("%s %s is available (you have %s).\n\nThis copy is not installed, so it cannot update itself. Open the download page?",
			appName, r.Version, a.version), appName, co.MB_YESNO|co.MB_ICONINFORMATION)
		if ret == co.ID_YES {
			shellOpen(h, repoURL+"/releases/latest")
		}
		return
	}
	ret, _ := h.MessageBox(fmt.Sprintf("%s %s is available (you have %s).\n\nDownload and install it now? %s closes during the installation.",
		appName, r.Version, a.version, appName), appName, co.MB_YESNO|co.MB_ICONINFORMATION)
	if ret != co.ID_YES || !a.confirmSave() {
		return
	}
	restore = waitCursor()
	path, err := update.Download(context.Background(), r, os.TempDir(), nil)
	restore()
	if err == nil {
		log := filepath.Join(os.TempDir(), "word4win-update.log")
		err = exec.Command("msiexec", "/i", path, "/l*v", log).Start()
	}
	if err != nil {
		h.MessageBox("Could not install the update.\n\n"+err.Error(), appName, co.MB_ICONEXCLAMATION)
		return
	}
	a.re.SetModified(false)
	h.PostMessage(co.WM_CLOSE, 0, 0)
}
