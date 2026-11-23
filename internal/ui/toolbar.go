//go:build windows

package ui

// The menu row and the two toolbars of the classic WordPad: the standard
// toolbar (files, printing, find, clipboard, undo, insert) and the format
// bar (style, font, size, character formatting, alignment, lists). They are
// painted here, flat, with the Fluent icons, in the light or dark colours;
// the style, font and size lists are combo boxes on top of the format bar.

import (
	"syscall"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"
)

type rbKind int

const (
	rbSmall     rbKind = iota // icon only, with a tooltip
	rbSmallText               // icon with the label to its right
	rbCombo                   // a place for a combo box
	rbSep                     // a separator
)

type rbItem struct {
	id       uint16
	icon     string
	label    string
	tip      string
	kind     rbKind
	dropdown bool    // the whole button opens a menu
	split    bool    // the arrow part opens a menu
	swatch   *uint32 // a colour bar under the icon
	width    int     // combo width at 96 dpi
	combo    win.HWND

	rc, arrow win.RECT
	checked   bool
	disabled  bool
}

// Toolbar is the window with the menu row and the toolbars.
type Toolbar struct {
	ctl   *ui.Control
	menus []string
	rows  [][]*rbItem
	items map[uint16]*rbItem

	menuRects []win.RECT
	hot       *rbItem
	hotMenu   int
	openMenu  int
	pressed   *rbItem
	tooltip   win.HWND
	tipTexts  [][]uint16

	OnCommand func(id uint16)
	OnDrop    func(id uint16, at win.POINT) // a menu button; at is the screen point below it
	OnMenu    func(index int, at win.POINT)
	OnCombo   func(id uint16, notify uint16)
}

const (
	comboFont  uint16 = 0x7101
	comboSize  uint16 = 0x7102
	comboStyle uint16 = 0x7103
)

// Menu row indexes.
var menuNames = []string{"&File", "&Edit", "&View", "&Insert", "F&ormat", "&Help"}

func newToolbar(parent ui.Parent) *Toolbar {
	tb := &Toolbar{menus: menuNames, hotMenu: -1, openMenu: -1, items: map[uint16]*rbItem{}}
	tb.ctl = ui.NewControl(parent, ui.OptsControl().
		ClassName("Word4WinToolbar").
		ClassCursor(win.HCURSOR(loadCursor(idcArrow))).
		Style(co.WS_CHILD|co.WS_VISIBLE|co.WS_CLIPCHILDREN).
		Size(400, 90))
	tb.build()
	tb.events()
	return tb
}

func (tb *Toolbar) Hwnd() win.HWND { return tb.ctl.Hwnd() }

const (
	menuRowH = 24
	barRowH  = 34
)

// Height is the height of the menu row and the two toolbars.
func (tb *Toolbar) Height() int32 { return px(menuRowH) + 2*px(barRowH) + 1 }

func small(id uint16, icon, tip string) *rbItem {
	return &rbItem{id: id, icon: icon, tip: tip, kind: rbSmall}
}

func sep() *rbItem { return &rbItem{kind: rbSep} }

func (tb *Toolbar) build() {
	textColor := uint32(0x0000C0)
	highlight := uint32(0x00FFFF)
	paste := small(cmdPaste, "clipboard_paste", "Paste (Ctrl+V)")
	paste.split = true
	tc := small(cmdTextColor, "text_color", "Text color")
	tc.split, tc.swatch = true, &textColor
	hl := small(cmdHighlight, "highlight", "Text highlight color")
	hl.split, hl.swatch = true, &highlight
	bullets := small(cmdBullets, "text_bullet_list_ltr", "Bullets (Ctrl+Shift+L)")
	bullets.split = true
	numbers := small(cmdNumbering, "text_number_list_ltr", "Numbering")
	numbers.split = true
	table := small(cmdTable, "table_add", "Insert a table")
	table.dropdown = true

	tb.rows = [][]*rbItem{
		{
			small(cmdNew, "document", "New (Ctrl+N)"), small(cmdOpen, "folder_open", "Open (Ctrl+O)"), small(cmdSave, "save", "Save (Ctrl+S)"), sep(),
			small(cmdPrint, "print", "Print (Ctrl+P)"), small(cmdPreview, "document_search", "Print preview (Ctrl+F2)"),
			small(cmdExportPDF, "document_pdf", "Export to PDF"), sep(),
			small(cmdFind, "search", "Find (Ctrl+F)"), sep(),
			small(cmdCut, "cut", "Cut (Ctrl+X)"), small(cmdCopy, "copy", "Copy (Ctrl+C)"), paste,
			small(cmdUndo, "arrow_undo", "Undo (Ctrl+Z)"), small(cmdRedo, "arrow_redo", "Redo (Ctrl+Y)"), sep(),
			small(cmdDateTime, "calendar_date", "Date and time"), small(cmdPicture, "image_add", "Insert a picture"),
			small(cmdLink, "link", "Insert a link (Ctrl+K)"), table, small(cmdRule, "line_horizontal_1", "Insert a horizontal line"),
		},
		{
			{id: comboStyle, kind: rbCombo, width: 104, tip: "Paragraph style"},
			{id: comboFont, kind: rbCombo, width: 150, tip: "Font"},
			{id: comboSize, kind: rbCombo, width: 52, tip: "Font size"}, sep(),
			small(cmdBold, "text_bold", "Bold (Ctrl+B)"), small(cmdItalic, "text_italic", "Italic (Ctrl+I)"),
			small(cmdUnderline, "text_underline", "Underline (Ctrl+U)"), small(cmdStrike, "text_strikethrough", "Strikethrough"),
			small(cmdSubscript, "text_subscript", "Subscript (Ctrl+=)"), small(cmdSuperscript, "text_superscript", "Superscript (Ctrl+Shift+=)"),
			tc, hl, sep(),
			small(cmdAlignLeft, "text_align_left", "Align left (Ctrl+L)"), small(cmdAlignCenter, "text_align_center", "Center (Ctrl+E)"),
			small(cmdAlignRight, "text_align_right", "Align right (Ctrl+R)"), small(cmdJustify, "text_align_justify", "Justify (Ctrl+J)"), sep(),
			bullets, numbers, small(cmdOutdent, "text_indent_decrease", "Decrease indent"), small(cmdIndent, "text_indent_increase", "Increase indent"),
		},
	}
	for _, row := range tb.rows {
		for _, it := range row {
			if it.kind != rbSep {
				tb.items[it.id] = it
			}
		}
	}
}

// SetSwatch sets the colour bar of the text and highlight colour buttons.
func (tb *Toolbar) SetSwatch(id uint16, c uint32) {
	if it := tb.items[id]; it != nil && it.swatch != nil && *it.swatch != c {
		*it.swatch = c
		invalidate(tb.Hwnd())
	}
}

// SetChecked and SetEnabled change buttons; they repaint when needed.
func (tb *Toolbar) SetChecked(id uint16, on bool) {
	if it := tb.items[id]; it != nil && it.checked != on {
		it.checked = on
		procInvalidateRect.Call(uintptr(tb.Hwnd()), uintptr(unsafe.Pointer(&it.rc)), 0)
	}
}

func (tb *Toolbar) SetEnabled(id uint16, on bool) {
	if it := tb.items[id]; it != nil && it.disabled == on {
		it.disabled = !on
		procInvalidateRect.Call(uintptr(tb.Hwnd()), uintptr(unsafe.Pointer(&it.rc)), 0)
	}
}

// Combo returns a combo box.
func (tb *Toolbar) Combo(id uint16) win.HWND {
	if it := tb.items[id]; it != nil {
		return it.combo
	}
	return 0
}

// MenuPoint is the screen point below a button of the menu row.
func (tb *Toolbar) MenuPoint(index int) win.POINT {
	if index < 0 || index >= len(tb.menuRects) {
		return win.POINT{}
	}
	r := tb.menuRects[index]
	at := win.POINT{X: r.Left, Y: r.Bottom}
	tb.Hwnd().ClientToScreenPt(&at)
	return at
}

// ShowMenu opens a menu of the menu row, with its button shown pressed.
func (tb *Toolbar) ShowMenu(index int) {
	if tb.OnMenu == nil || index < 0 || index >= len(tb.menus) {
		return
	}
	tb.openMenu = index
	invalidate(tb.Hwnd())
	tb.Hwnd().UpdateWindow()
	tb.OnMenu(index, tb.MenuPoint(index))
	tb.openMenu = -1
	invalidate(tb.Hwnd())
}

func (tb *Toolbar) events() {
	on := tb.ctl.On()
	on.WmCreate(func(_ ui.WmCreate) int {
		tb.createCombos()
		tb.createTooltip()
		return 0
	})
	on.WmSize(func(_ ui.WmSize) { tb.layout() })
	on.WmEraseBkgnd(func(_ ui.WmEraseBkgnd) int { return 1 })
	on.WmPaint(tb.paint)
	on.WmMouseMove(func(p ui.WmMouse) {
		it, menu := tb.hit(p.Pos())
		if it != tb.hot || menu != tb.hotMenu {
			tb.hot, tb.hotMenu = it, menu
			invalidate(tb.Hwnd())
		}
		trackLeave(tb.Hwnd())
	})
	on.Wm(co.WM_MOUSELEAVE, func(_ ui.Wm) uintptr {
		if tb.hot != nil || tb.hotMenu >= 0 {
			tb.hot, tb.hotMenu = nil, -1
			invalidate(tb.Hwnd())
		}
		return 0
	})
	on.WmLButtonDown(func(p ui.WmMouse) {
		pt := p.Pos()
		it, menu := tb.hit(pt)
		if menu >= 0 {
			tb.hotMenu = -1
			tb.ShowMenu(menu)
			return
		}
		if it == nil || it.disabled {
			return
		}
		if it.dropdown || it.split && inRect(it.arrow, pt) {
			tb.pressed = it
			invalidate(tb.Hwnd())
			at := win.POINT{X: it.rc.Left, Y: it.rc.Bottom}
			tb.Hwnd().ClientToScreenPt(&at)
			if tb.OnDrop != nil {
				tb.OnDrop(it.id, at)
			}
			tb.pressed = nil
			invalidate(tb.Hwnd())
			return
		}
		tb.pressed = it
		procSetCapture.Call(uintptr(tb.Hwnd()))
		invalidate(tb.Hwnd())
	})
	on.WmLButtonUp(func(p ui.WmMouse) {
		procReleaseCapture.Call()
		it := tb.pressed
		tb.pressed = nil
		invalidate(tb.Hwnd())
		if it == nil {
			return
		}
		if hit, _ := tb.hit(p.Pos()); hit == it && tb.OnCommand != nil {
			tb.OnCommand(it.id)
		}
	})
	on.WmLButtonDblClk(func(p ui.WmMouse) {
		if it, _ := tb.hit(p.Pos()); it != nil && !it.dropdown && !it.disabled && tb.OnCommand != nil {
			tb.OnCommand(it.id)
		}
	})
	for _, id := range []uint16{comboFont, comboSize, comboStyle} {
		for _, code := range []co.CMD{co.CMD(co.CBN_SELCHANGE), co.CMD(co.CBN_SELENDOK)} {
			on.WmCommand(id, code, func() {
				if tb.OnCombo != nil {
					tb.OnCombo(id, uint16(code))
				}
			})
		}
	}
}

func (tb *Toolbar) createCombos() {
	hInst, _ := tb.Hwnd().HInstance()
	font := uiFont(0, 0)
	for _, id := range []uint16{comboFont, comboSize, comboStyle} {
		style := uintptr(co.WS_CHILD|co.WS_VISIBLE|co.WS_VSCROLL|co.WS_TABSTOP) | 0x0002 | 0x0040 // CBS_DROPDOWN | CBS_AUTOHSCROLL
		if id == comboFont {
			style |= 0x0100 // CBS_SORT
		}
		if id == comboStyle {
			style = uintptr(co.WS_CHILD|co.WS_VISIBLE|co.WS_VSCROLL|co.WS_TABSTOP) | 0x0003 // CBS_DROPDOWNLIST
		}
		h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("COMBOBOX"))), 0, style,
			0, 0, 100, uintptr(px(300)), uintptr(tb.Hwnd()), uintptr(id), uintptr(hInst), 0)
		hw := win.HWND(h)
		hw.SendMessage(co.WM_SETFONT, win.WPARAM(font), 1)
		tb.items[id].combo = hw
	}
	for _, s := range styleNames {
		comboAdd(tb.items[comboStyle].combo, s)
	}
	size := tb.items[comboSize].combo
	for _, s := range []string{"8", "9", "10", "10.5", "11", "12", "14", "16", "18", "20", "22", "24", "26", "28", "36", "48", "72"} {
		comboAdd(size, s)
	}
	size.SendMessage(co.CB_LIMITTEXT, 5, 0)
	fillFontCombo(tb.items[comboFont].combo)
}

var styleNames = []string{"Normal", "Heading 1", "Heading 2", "Heading 3", "Heading 4", "Heading 5", "Heading 6", "Quote", "Code"}

func comboAdd(h win.HWND, s string) {
	h.SendMessage(co.CB_ADDSTRING, 0, win.LPARAM(unsafe.Pointer(utf16Ptr(s))))
}

// fillFontCombo lists the installed font families, as GDI knows them.
func fillFontCombo(h win.HWND) {
	seen := map[string]bool{}
	hdc, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, hdc)
	lf := logFont{CharSet: 1} // DEFAULT_CHARSET
	cb := syscall.NewCallback(func(elf *logFont, tm uintptr, fontType uint32, lparam uintptr) uintptr {
		name := elf.face()
		if name != "" && name[0] != '@' && !seen[name] {
			seen[name] = true
			comboAdd(h, name)
		}
		return 1
	})
	procEnumFontFamiliesExW.Call(hdc, uintptr(unsafe.Pointer(&lf)), cb, 0, 0)
}

func (tb *Toolbar) createTooltip() {
	hInst, _ := tb.Hwnd().HInstance()
	h, _, _ := procCreateWindowExW.Call(0x00000008, uintptr(unsafe.Pointer(utf16Ptr("tooltips_class32"))), 0,
		uintptr(co.WS_POPUP)|0x01|0x02, // TTS_ALWAYSTIP | TTS_NOPREFIX
		0x80000000, 0x80000000, 0x80000000, 0x80000000, uintptr(tb.Hwnd()), 0, uintptr(hInst), 0)
	tb.tooltip = win.HWND(h)
	tb.tooltip.SendMessage(co.WM_SETFONT, win.WPARAM(uiFont(0, 0)), 0)
}

// toolInfo is TOOLINFOW.
type toolInfo struct {
	size     uint32
	flags    uint32
	hwnd     uintptr
	id       uintptr
	rect     win.RECT
	hinst    uintptr
	text     *uint16
	lparam   uintptr
	reserved uintptr
}

const (
	ttmAddTool  = wmUser + 50
	ttmDelTool  = wmUser + 51
	ttfSubclass = 0x0010
)

// updateTooltips puts a tool on every button.
func (tb *Toolbar) updateTooltips() {
	if tb.tooltip == 0 {
		return
	}
	for i := range tb.tipTexts {
		ti := toolInfo{hwnd: uintptr(tb.Hwnd()), id: uintptr(i + 1)}
		ti.size = uint32(unsafe.Sizeof(ti))
		tb.tooltip.SendMessage(co.WM(ttmDelTool), 0, win.LPARAM(unsafe.Pointer(&ti)))
	}
	tb.tipTexts = tb.tipTexts[:0]
	for _, row := range tb.rows {
		for _, it := range row {
			if it.tip == "" || it.kind == rbCombo || it.kind == rbSep {
				continue
			}
			text, _ := syscall.UTF16FromString(it.tip)
			tb.tipTexts = append(tb.tipTexts, text)
			ti := toolInfo{flags: ttfSubclass, hwnd: uintptr(tb.Hwnd()), id: uintptr(len(tb.tipTexts)), rect: it.rc, text: &text[0]}
			ti.size = uint32(unsafe.Sizeof(ti))
			tb.tooltip.SendMessage(co.WM(ttmAddTool), 0, win.LPARAM(unsafe.Pointer(&ti)))
		}
	}
}

func (tb *Toolbar) hit(pt win.POINT) (*rbItem, int) {
	for i, r := range tb.menuRects {
		if inRect(r, pt) {
			return nil, i
		}
	}
	for _, row := range tb.rows {
		for _, it := range row {
			if (it.kind == rbSmall || it.kind == rbSmallText) && inRect(it.rc, pt) {
				return it, -1
			}
		}
	}
	return nil, -1
}

// layout places the menu buttons and the toolbar buttons.
func (tb *Toolbar) layout() {
	h := tb.Hwnd()
	hdc, _, _ := procGetDC.Call(uintptr(h))
	defer procReleaseDC.Call(uintptr(h), hdc)
	font := uiFont(0, 0)
	tb.menuRects = tb.menuRects[:0]
	x := px(4)
	for _, m := range tb.menus {
		w, _ := textSize(hdc, font, stripAmp(m))
		w += px(16)
		tb.menuRects = append(tb.menuRects, win.RECT{Left: x, Top: px(1), Right: x + w, Bottom: px(menuRowH) - 1})
		x += w
	}
	for r, row := range tb.rows {
		top := px(menuRowH) + int32(r)*px(barRowH)
		x := px(6)
		for _, it := range row {
			var w int32
			switch it.kind {
			case rbSep:
				w = px(9)
				it.rc = win.RECT{Left: x, Top: top + px(6), Right: x + w, Bottom: top + px(barRowH) - px(6)}
			case rbCombo:
				w = px(it.width)
				it.rc = win.RECT{Left: x, Top: top, Right: x + w, Bottom: top + px(barRowH)}
				if it.combo != 0 {
					it.combo.SetWindowPos(0, win.POINT{X: x, Y: top + (px(barRowH)-px(23))/2}, win.SIZE{Cx: w, Cy: px(300)}, co.SWP_NOZORDER|co.SWP_NOACTIVATE)
				}
				w += px(4)
			default:
				w = px(30)
				if it.split || it.dropdown {
					w += px(12)
				}
				it.rc = win.RECT{Left: x, Top: top + px(3), Right: x + w, Bottom: top + px(barRowH) - px(3)}
				if it.split {
					it.arrow = win.RECT{Left: it.rc.Right - px(13), Top: it.rc.Top, Right: it.rc.Right, Bottom: it.rc.Bottom}
				}
				w += px(1)
			}
			x += w
		}
	}
	tb.updateTooltips()
}

func stripAmp(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r != '&' {
			out = append(out, r)
		}
	}
	return string(out)
}

func (tb *Toolbar) paint() {
	h := tb.Hwnd()
	var ps win.PAINTSTRUCT
	hdcW, err := h.BeginPaint(&ps)
	if err != nil {
		return
	}
	defer h.EndPaint(&ps)
	rc, _ := h.GetClientRect()
	buf := newBuffer(uintptr(hdcW), rc.Right, rc.Bottom)
	hdc := buf.hdc
	fillRect(hdc, &rc, colors.bar)
	old, _, _ := procSelectObject.Call(hdc, uiFont(0, 0))

	// The menu row; the underline of the access key shows with Alt, as in
	// Windows menus.
	for i, r := range tb.menuRects {
		if i == tb.openMenu {
			roundRect(hdc, r, px(4), colors.pressed, 0xFFFFFFFF)
		} else if i == tb.hotMenu {
			roundRect(hdc, r, px(4), colors.hot, 0xFFFFFFFF)
		}
		u, _ := syscall.UTF16FromString(tb.menus[i])
		procSetBkMode.Call(hdc, 1)
		procSetTextColor.Call(hdc, uintptr(colors.text))
		flags := uintptr(dtCenter | dtVCenter | dtSingleLine)
		if !keyDown(co.VK_MENU) {
			flags |= 0x00100000 // DT_HIDEPREFIX
		}
		rr := r
		procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&u[0])), ^uintptr(0), uintptr(unsafe.Pointer(&rr)), flags)
	}
	for r := range tb.rows {
		y := px(menuRowH) + int32(r)*px(barRowH)
		line(hdc, 0, y, rc.Right, y, colors.barLine)
	}
	for _, row := range tb.rows {
		for _, it := range row {
			switch it.kind {
			case rbSep:
				cx := (it.rc.Left + it.rc.Right) / 2
				line(hdc, cx, it.rc.Top, cx, it.rc.Bottom, colors.barLine)
			case rbCombo:
			default:
				paintButton(hdc, it, tb.hot == it, tb.pressed == it)
			}
		}
	}
	line(hdc, 0, rc.Bottom-1, rc.Right, rc.Bottom-1, colors.barLine)
	procSelectObject.Call(hdc, old)
	buf.blit(uintptr(hdcW), 0, 0)
}

// paintButton draws a button of a toolbar.
func paintButton(hdc uintptr, it *rbItem, hot, pressed bool) {
	r := it.rc
	fg := colors.text
	if it.disabled {
		fg = colors.textDim
	}
	bg := uint32(0xFFFFFFFF)
	switch {
	case it.disabled:
	case pressed:
		bg = colors.pressed
	case it.checked:
		bg = colors.checked
	case hot:
		bg = colors.hot
	}
	if bg != 0xFFFFFFFF {
		border := uint32(0xFFFFFFFF)
		if it.checked {
			border = colors.checkedLine
		}
		roundRect(hdc, r, px(6), bg, border)
		if it.split && hot && !it.checked {
			line(hdc, it.arrow.Left, r.Top+px(4), it.arrow.Left, r.Bottom-px(4), colors.barLine)
		}
	}
	size := int(px(20))
	switch it.kind {
	case rbSmall:
		right := r.Right
		if it.split || it.dropdown {
			right -= px(12)
			drawIcon(hdc, "chevron_down", right, (r.Top+r.Bottom-px(10))/2, int(px(10)), fg)
		}
		ix := (r.Left + right - int32(size)) / 2
		iy := (r.Top + r.Bottom - int32(size)) / 2
		if it.swatch != nil {
			iy -= px(2)
		}
		drawIcon(hdc, it.icon, ix, iy, size, fg)
		if it.swatch != nil {
			bar := win.RECT{Left: ix + px(2), Top: iy + int32(size) - px(1), Right: ix + int32(size) - px(2), Bottom: iy + int32(size) + px(3)}
			fillRect(hdc, &bar, *it.swatch)
		}
	case rbSmallText:
		drawIcon(hdc, it.icon, r.Left+px(6), (r.Top+r.Bottom-int32(size))/2, size, fg)
		tr := win.RECT{Left: r.Left + px(32), Top: r.Top, Right: r.Right, Bottom: r.Bottom}
		drawText(hdc, it.label, tr, dtVCenter|dtSingleLine, fg)
	}
}

func smallText(id uint16, icon, label, tip string) *rbItem {
	return &rbItem{id: id, icon: icon, label: label, tip: tip, kind: rbSmallText}
}

func inRect(rc win.RECT, pt win.POINT) bool {
	return pt.X >= rc.Left && pt.X < rc.Right && pt.Y >= rc.Top && pt.Y < rc.Bottom
}
