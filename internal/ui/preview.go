//go:build windows

package ui

// The print preview: the pages as they will be printed, with the print
// scale (which also applies to printing and PDF export), the zoom, one or
// two pages side by side, and buttons to print and set up the page.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unsafe"

	"github.com/mevdschee/word4win/internal/doc"
	"github.com/mevdschee/word4win/internal/layout"
	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"
)

const (
	pvPrint uint16 = 0x7201 + iota
	pvSetup
	pvPrev
	pvNext
	pvOne
	pvTwo
	pvZoomOut
	pvZoomIn
	pvFit
	pvPDF
	pvClose
	pvScale
)

type preview struct {
	a      *App
	wnd    *ui.Modal
	bar    *ui.Control
	canvas *ui.Control
	scale  win.HWND
	model  *doc.Document
	pages  []*layout.Page

	items   []*rbItem
	hot     *rbItem
	pressed *rbItem
	label   win.RECT

	zoom               float64 // pixels per point
	fit                bool
	two                bool
	scroll             win.POINT
	contentW, contentH int32
}

func (a *App) preview() {
	p := &preview{a: a, model: a.model(), fit: true}
	p.relayout()
	// As big as the main window, which the preview covers.
	cr, _ := a.wnd.Hwnd().GetClientRect()
	w, h := int(cr.Right), int(cr.Bottom)
	p.wnd = newModal(a.wnd, ui.OptsModal().
		Title("Print preview - "+a.name()).
		Size(mulDiv(w, 96, screenDpi()), mulDiv(h, 96, screenDpi())).
		Style(co.WS_CAPTION|co.WS_SYSMENU|co.WS_THICKFRAME|co.WS_MAXIMIZEBOX|co.WS_CLIPCHILDREN|co.WS_VISIBLE))
	p.bar = ui.NewControl(p.wnd, ui.OptsControl().ClassName("Word4WinPreviewBar").
		ClassCursor(win.HCURSOR(loadCursor(idcArrow))).Style(co.WS_CHILD|co.WS_VISIBLE|co.WS_CLIPCHILDREN).Size(100, 40))
	p.canvas = ui.NewControl(p.wnd, ui.OptsControl().ClassName("Word4WinPreviewCanvas").
		ClassCursor(win.HCURSOR(loadCursor(idcArrow))).Style(co.WS_CHILD|co.WS_VISIBLE|co.WS_VSCROLL|co.WS_HSCROLL).Size(100, 100))
	p.items = []*rbItem{
		smallText(pvPrint, "print", "Print", "Print (Ctrl+P)"),
		smallText(pvSetup, "settings", "Page setup", "Page setup"),
		smallText(pvPDF, "document_pdf", "PDF", "Export to PDF"),
		small(pvPrev, "chevron_left", "Previous page (Page Up)"),
		small(pvNext, "chevron_right", "Next page (Page Down)"),
		small(pvOne, "document_one_page", "One page"),
		small(pvTwo, "document_landscape_split", "Two pages"),
		small(pvZoomOut, "zoom_out", "Zoom out (Ctrl+Num -)"),
		small(pvZoomIn, "zoom_in", "Zoom in (Ctrl+Num +)"),
		small(pvFit, "page_fit", "Whole page"),
		{id: pvScale, kind: rbCombo, width: 76, tip: "Print scale"},
		smallText(pvClose, "door_arrow_left", "Close", "Close print preview (Esc)"),
	}
	p.events()
	p.wnd.ShowModal()
	a.re.Hwnd().SetFocus()
}

func (p *preview) relayout() {
	p.pages = layout.Layout(p.model, p.a.layoutSettings())
}

func (p *preview) events() {
	p.wnd.On().WmCreate(func(_ ui.WmCreate) int {
		hInst, _ := p.bar.Hwnd().HInstance()
		h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("COMBOBOX"))), 0,
			uintptr(co.WS_CHILD|co.WS_VISIBLE|co.WS_VSCROLL|co.WS_TABSTOP)|0x0002, 0, 0, 100, uintptr(px(300)),
			uintptr(p.bar.Hwnd()), uintptr(pvScale), uintptr(hInst), 0)
		p.scale = win.HWND(h)
		p.scale.SendMessage(co.WM_SETFONT, win.WPARAM(uiFont(0, 0)), 1)
		for _, s := range []int{25, 50, 75, 80, 90, 100, 110, 125, 150, 175, 200} {
			comboAdd(p.scale, strconv.Itoa(s)+"%")
		}
		setWindowText(p.scale, strconv.Itoa(p.a.cfg.PrintScale)+"%")
		themeControl(p.scale)
		p.layout()
		return 0
	})
	p.wnd.On().WmSize(func(_ ui.WmSize) { p.layout() })
	p.wnd.On().WmCommandAccelMenu(uint16(co.ID_CANCEL), func() { closeModal(p.wnd) })
	p.wnd.On().WmCommandAccelMenu(uint16(co.ID_OK), func() { p.applyScale() })

	bar := p.bar.On()
	bar.WmEraseBkgnd(func(_ ui.WmEraseBkgnd) int { return 1 })
	bar.WmPaint(p.paintBar)
	bar.WmMouseMove(func(m ui.WmMouse) {
		it := p.hit(m.Pos())
		if it != p.hot {
			p.hot = it
			invalidate(p.bar.Hwnd())
		}
		trackLeave(p.bar.Hwnd())
	})
	bar.Wm(co.WM_MOUSELEAVE, func(_ ui.Wm) uintptr {
		p.hot = nil
		invalidate(p.bar.Hwnd())
		return 0
	})
	bar.WmLButtonDown(func(m ui.WmMouse) {
		p.pressed = p.hit(m.Pos())
		invalidate(p.bar.Hwnd())
	})
	bar.WmLButtonUp(func(m ui.WmMouse) {
		it := p.pressed
		p.pressed = nil
		invalidate(p.bar.Hwnd())
		if it != nil && p.hit(m.Pos()) == it && !it.disabled {
			p.command(it.id)
		}
	})
	for _, code := range []co.CMD{co.CMD(co.CBN_SELCHANGE), co.CMD(co.CBN_KILLFOCUS)} {
		bar.WmCommand(pvScale, code, func() {
			if code == co.CMD(co.CBN_SELCHANGE) {
				sel, _ := p.scale.SendMessage(co.CB_GETCURSEL, 0, 0)
				if int(sel) >= 0 {
					setWindowText(p.scale, comboItem(p.scale, int(sel)))
				}
			}
			p.applyScale()
		})
	}

	cv := p.canvas.On()
	cv.WmEraseBkgnd(func(_ ui.WmEraseBkgnd) int { return 1 })
	cv.WmPaint(p.paint)
	cv.WmSize(func(_ ui.WmSize) { p.updateScroll() })
	cv.WmVScroll(func(m ui.WmScroll) { p.onScroll(1, m.Request(), m.Raw.WParam) })
	cv.WmHScroll(func(m ui.WmScroll) { p.onScroll(0, m.Request(), m.Raw.WParam) })
	cv.Wm(co.WM(wmMouseWheel), func(m ui.Wm) uintptr {
		delta := int16(m.WParam >> 16)
		if keyDown(co.VK_CONTROL) {
			if delta > 0 {
				p.command(pvZoomIn)
			} else {
				p.command(pvZoomOut)
			}
			return 0
		}
		p.scrollTo(p.scroll.X, p.scroll.Y-int32(delta)*px(60)/120)
		return 0
	})
	cv.WmLButtonDown(func(_ ui.WmMouse) { p.canvas.Hwnd().SetFocus() })
	cv.WmKeyDown(func(k ui.WmKey) {
		switch k.VirtualKeyCode() {
		case co.VK_NEXT:
			p.command(pvNext)
		case co.VK_PRIOR:
			p.command(pvPrev)
		case co.VK_HOME:
			p.scrollTo(p.scroll.X, 0)
		case co.VK_END:
			p.scrollTo(p.scroll.X, p.contentH)
		case co.VK_DOWN:
			p.scrollTo(p.scroll.X, p.scroll.Y+px(40))
		case co.VK_UP:
			p.scrollTo(p.scroll.X, p.scroll.Y-px(40))
		case co.VK_ESCAPE:
			closeModal(p.wnd)
		case co.VK_OEM_PLUS, co.VK_ADD:
			if keyDown(co.VK_CONTROL) {
				p.command(pvZoomIn)
			}
		case co.VK_OEM_MINUS, co.VK_SUBTRACT:
			if keyDown(co.VK_CONTROL) {
				p.command(pvZoomOut)
			}
		case co.VK('P'):
			if keyDown(co.VK_CONTROL) {
				p.command(pvPrint)
			}
		}
	})
	cv.Wm(co.WM_GETDLGCODE, func(_ ui.Wm) uintptr { return 0x0001 | 0x0004 }) // arrows, all keys
}

func (p *preview) applyScale() {
	text := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(windowText(p.scale)), "%"))
	v, err := strconv.Atoi(text)
	if err != nil || v < 10 || v > 400 {
		setWindowText(p.scale, strconv.Itoa(p.a.cfg.PrintScale)+"%")
		return
	}
	if v != p.a.cfg.PrintScale {
		p.a.cfg.PrintScale = v
		p.relayout()
		p.updateScroll()
		invalidate(p.canvas.Hwnd())
		invalidate(p.bar.Hwnd())
	}
	setWindowText(p.scale, strconv.Itoa(v)+"%")
}

func (p *preview) command(id uint16) {
	switch id {
	case pvPrint:
		closeModal(p.wnd)
		p.a.printDialog()
	case pvSetup:
		p.a.pageSetup()
		setWindowText(p.scale, strconv.Itoa(p.a.cfg.PrintScale)+"%")
		p.relayout()
		p.updateScroll()
	case pvPDF:
		p.a.exportPDF()
	case pvPrev, pvNext:
		cur := p.currentPage()
		step := 1
		if p.two {
			step = 2
		}
		if id == pvPrev {
			cur -= step
		} else {
			cur += step
		}
		cur = max(0, min(len(p.pages)-1, cur))
		y, _ := p.pagePos(cur)
		p.scrollTo(p.scroll.X, y.Y-px(16))
	case pvOne, pvTwo:
		p.two = id == pvTwo
		p.updateScroll()
	case pvZoomIn, pvZoomOut:
		z := p.zoomNow()
		if id == pvZoomIn {
			z *= 1.25
		} else {
			z /= 1.25
		}
		p.zoom = math.Max(0.1, math.Min(z, 8))
		p.fit = false
		p.updateScroll()
	case pvFit:
		p.fit = true
		p.updateScroll()
	case pvClose:
		closeModal(p.wnd)
		return
	}
	invalidate(p.canvas.Hwnd())
	invalidate(p.bar.Hwnd())
}

func (p *preview) hit(pt win.POINT) *rbItem {
	for _, it := range p.items {
		if it.kind != rbCombo && inRect(it.rc, pt) {
			return it
		}
	}
	return nil
}

func (p *preview) layout() {
	rc, _ := p.wnd.Hwnd().GetClientRect()
	barH := px(44)
	p.bar.Hwnd().SetWindowPos(0, win.POINT{}, win.SIZE{Cx: rc.Right, Cy: barH}, co.SWP_NOZORDER)
	p.canvas.Hwnd().SetWindowPos(0, win.POINT{Y: barH}, win.SIZE{Cx: rc.Right, Cy: max(0, rc.Bottom-barH)}, co.SWP_NOZORDER)
	hdc, _, _ := procGetDC.Call(uintptr(p.bar.Hwnd()))
	defer procReleaseDC.Call(uintptr(p.bar.Hwnd()), hdc)
	x := px(8)
	y := px(8)
	for i, it := range p.items {
		var w int32
		switch it.kind {
		case rbSmallText:
			tw, _ := textSize(hdc, uiFont(0, 0), it.label)
			w = px(36) + tw
		case rbSmall:
			w = px(32)
		case rbCombo:
			w = px(it.width)
			if p.scale != 0 {
				tw, _ := textSize(hdc, uiFont(0, 0), "Scale")
				x += tw + px(6)
				p.scale.SetWindowPos(0, win.POINT{X: x, Y: y + px(2)}, win.SIZE{Cx: w, Cy: px(300)}, co.SWP_NOZORDER)
			}
		}
		if it.id == pvClose {
			x = max(x, rc.Right-w-px(8))
		}
		it.rc = win.RECT{Left: x, Top: y, Right: x + w, Bottom: y + px(28)}
		x += w + px(2)
		if it.id == pvPDF || it.id == pvTwo || it.id == pvFit {
			x += px(10) // a gap between groups
		}
		if it.id == pvNext {
			p.label = win.RECT{Left: x, Top: y, Right: x + px(96), Bottom: y + px(28)}
			x += px(100)
		}
		_ = i
	}
	p.updateScroll()
}

func (p *preview) paintBar() {
	h := p.bar.Hwnd()
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
	for _, it := range p.items {
		switch it.id {
		case pvOne:
			it.checked = !p.two
		case pvTwo:
			it.checked = p.two
		case pvFit:
			it.checked = p.fit
		case pvPrev:
			it.disabled = p.currentPage() == 0
		case pvNext:
			it.disabled = p.currentPage() >= len(p.pages)-1
		}
		if it.kind == rbCombo {
			drawText(hdc, "Scale", win.RECT{Left: it.rc.Left, Top: it.rc.Top, Right: it.rc.Left + px(60), Bottom: it.rc.Bottom}, dtVCenter|dtSingleLine, colors.text)
			continue
		}
		paintButton(hdc, it, p.hot == it, p.pressed == it)
	}
	drawText(hdc, fmt.Sprintf("Page %d of %d", p.currentPage()+1, len(p.pages)), p.label, dtCenter|dtVCenter|dtSingleLine, colors.text)
	line(hdc, 0, rc.Bottom-1, rc.Right, rc.Bottom-1, colors.barLine)
	procSelectObject.Call(hdc, old)
	buf.blit(uintptr(hdcW), 0, 0)
}

const pageGap = 24

// zoomNow is the zoom in pixels per point; with fit, the zoom that shows
// whole pages.
func (p *preview) zoomNow() float64 {
	if !p.fit || len(p.pages) == 0 {
		if p.zoom == 0 {
			p.zoom = float64(screenDpi()) / 72
		}
		return p.zoom
	}
	rc, _ := p.canvas.Hwnd().GetClientRect()
	pw, ph := p.pages[0].Width, p.pages[0].Height
	cols := 1.0
	if p.two {
		cols = 2
	}
	gap := float64(px(pageGap))
	zw := (float64(rc.Right) - gap*(cols+1)) / (pw * cols)
	zh := (float64(rc.Bottom) - 2*gap) / ph
	return math.Max(0.05, math.Min(zw, zh))
}

// pagePos is the position of a page in the content, and its size.
func (p *preview) pagePos(i int) (win.POINT, win.SIZE) {
	z := p.zoomNow()
	gap := px(pageGap)
	pg := p.pages[i]
	w, h := int32(pg.Width*z), int32(pg.Height*z)
	row, col := i, 0
	cols := int32(1)
	if p.two {
		row, col = i/2, i%2
		cols = 2
	}
	rowW := cols*w + (cols-1)*gap
	rc, _ := p.canvas.Hwnd().GetClientRect()
	left := max(gap, (rc.Right-rowW)/2)
	return win.POINT{X: left + int32(col)*(w+gap), Y: gap + int32(row)*(h+gap)}, win.SIZE{Cx: w, Cy: h}
}

func (p *preview) currentPage() int {
	if len(p.pages) == 0 {
		return 0
	}
	cur := 0
	for i := range p.pages {
		pos, sz := p.pagePos(i)
		if pos.Y+sz.Cy/2 <= p.scroll.Y+px(pageGap) || pos.Y <= p.scroll.Y+px(pageGap) {
			cur = i
		}
	}
	return cur
}

func (p *preview) updateScroll() {
	if p.canvas == nil || len(p.pages) == 0 {
		return
	}
	last := len(p.pages) - 1
	pos, sz := p.pagePos(last)
	p.contentH = pos.Y + sz.Cy + px(pageGap)
	_, sz0 := p.pagePos(0)
	cols := int32(1)
	if p.two {
		cols = 2
	}
	p.contentW = cols*sz0.Cx + (cols+1)*px(pageGap)
	rc, _ := p.canvas.Hwnd().GetClientRect()
	set := func(bar int, total, page, pos int32) {
		si := struct {
			size, mask    uint32
			min, max      int32
			page          uint32
			pos, trackPos int32
		}{mask: 0x0001 | 0x0002 | 0x0004, min: 0, max: total - 1, page: uint32(max(0, page)), pos: pos} // SIF_RANGE|SIF_PAGE|SIF_POS
		si.size = uint32(unsafe.Sizeof(si))
		procSetScrollInfo.Call(uintptr(p.canvas.Hwnd()), uintptr(bar), uintptr(unsafe.Pointer(&si)), 1)
	}
	p.scroll.X = max(0, min(p.scroll.X, p.contentW-rc.Right))
	p.scroll.Y = max(0, min(p.scroll.Y, p.contentH-rc.Bottom))
	set(0, p.contentW, rc.Right, p.scroll.X)
	set(1, p.contentH, rc.Bottom, p.scroll.Y)
	invalidate(p.canvas.Hwnd())
	invalidate(p.bar.Hwnd())
}

func (p *preview) scrollTo(x, y int32) {
	rc, _ := p.canvas.Hwnd().GetClientRect()
	x = max(0, min(x, p.contentW-rc.Right))
	y = max(0, min(y, p.contentH-rc.Bottom))
	if x == p.scroll.X && y == p.scroll.Y {
		return
	}
	p.scroll = win.POINT{X: x, Y: y}
	p.updateScroll()
}

func (p *preview) onScroll(bar int, req co.SB_REQ, wParam win.WPARAM) {
	rc, _ := p.canvas.Hwnd().GetClientRect()
	pos := p.scroll.Y
	page := rc.Bottom
	if bar == 0 {
		pos, page = p.scroll.X, rc.Right
	}
	switch req {
	case co.SB_REQ_LINEUP:
		pos -= px(40)
	case co.SB_REQ_LINEDOWN:
		pos += px(40)
	case co.SB_REQ_PAGEUP:
		pos -= page
	case co.SB_REQ_PAGEDOWN:
		pos += page
	case co.SB_REQ_THUMBTRACK, co.SB_REQ_THUMBPOSITION:
		si := struct {
			size, mask    uint32
			min, max      int32
			page          uint32
			pos, trackPos int32
		}{mask: 0x0010} // SIF_TRACKPOS
		si.size = uint32(unsafe.Sizeof(si))
		procGetScrollInfo.Call(uintptr(p.canvas.Hwnd()), uintptr(bar), uintptr(unsafe.Pointer(&si)))
		pos = si.trackPos
	case co.SB_REQ_TOP:
		pos = 0
	case co.SB_REQ_BOTTOM:
		pos = 1 << 30
	}
	if bar == 0 {
		p.scrollTo(pos, p.scroll.Y)
	} else {
		p.scrollTo(p.scroll.X, pos)
	}
}

func (p *preview) paint() {
	h := p.canvas.Hwnd()
	var ps win.PAINTSTRUCT
	hdcW, err := h.BeginPaint(&ps)
	if err != nil {
		return
	}
	defer h.EndPaint(&ps)
	rc, _ := h.GetClientRect()
	buf := newBuffer(uintptr(hdcW), rc.Right, rc.Bottom)
	hdc := buf.hdc
	fillRect(hdc, &rc, colors.workspace)
	z := p.zoomNow()
	for i, pg := range p.pages {
		pos, sz := p.pagePos(i)
		x, y := pos.X-p.scroll.X, pos.Y-p.scroll.Y
		if y > rc.Bottom || y+sz.Cy < 0 {
			continue
		}
		shadow := win.RECT{Left: x + px(3), Top: y + px(3), Right: x + sz.Cx + px(3), Bottom: y + sz.Cy + px(3)}
		fillRect(hdc, &shadow, mix(colors.workspace, 0x000000, 0.25))
		page := win.RECT{Left: x, Top: y, Right: x + sz.Cx, Bottom: y + sz.Cy}
		fillRect(hdc, &page, 0xFFFFFF)
		procSaveDC.Call(hdc)
		procIntersectClipRect.Call(hdc, uintptr(page.Left), uintptr(page.Top), uintptr(page.Right), uintptr(page.Bottom))
		renderPage(hdc, pg, z, float64(x), float64(y))
		procRestoreDC.Call(hdc, ^uintptr(0)) // -1: the last saved state
	}
	buf.blit(uintptr(hdcW), 0, 0)
}
