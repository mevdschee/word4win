//go:build windows

package ui

// The status bar: the word and character count.

import (
	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"
)

type StatusBar struct {
	ctl  *ui.Control
	text string
}

func newStatusBar(parent ui.Parent) *StatusBar {
	sb := &StatusBar{}
	sb.ctl = ui.NewControl(parent, ui.OptsControl().
		ClassName("Word4WinStatus").
		ClassCursor(win.HCURSOR(loadCursor(idcArrow))).
		Style(co.WS_CHILD|co.WS_VISIBLE).
		Size(400, 24))
	on := sb.ctl.On()
	on.WmSize(func(_ ui.WmSize) { invalidate(sb.ctl.Hwnd()) })
	on.WmEraseBkgnd(func(_ ui.WmEraseBkgnd) int { return 1 })
	on.WmPaint(sb.paint)
	return sb
}

func (sb *StatusBar) Hwnd() win.HWND { return sb.ctl.Hwnd() }
func (sb *StatusBar) Height() int32  { return px(26) }

func (sb *StatusBar) SetText(s string) {
	if s != sb.text {
		sb.text = s
		invalidate(sb.ctl.Hwnd())
	}
}

func (sb *StatusBar) paint() {
	h := sb.ctl.Hwnd()
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
	line(hdc, 0, 0, rc.Right, 0, colors.barLine)
	old, _, _ := procSelectObject.Call(hdc, uiFont(0, 0))
	drawText(hdc, sb.text, win.RECT{Left: px(12), Top: 0, Right: rc.Right - px(12), Bottom: rc.Bottom}, dtVCenter|dtSingleLine|dtEndEllipsis, colors.text)
	procSelectObject.Call(hdc, old)
	buf.blit(uintptr(hdcW), 0, 0)
}
