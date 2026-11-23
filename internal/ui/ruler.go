//go:build windows

package ui

// The ruler of WordPad: the text width with marks in inches or
// centimetres, and the indents of the current paragraph, which can be
// dragged: the first line (the triangle at the top), the other lines (the
// triangle at the bottom), both (the box under it) and the right indent.

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"
)

type Ruler struct {
	ctl *ui.Control

	// Set by the app before painting.
	origin int32   // x of the text's left edge, in ruler pixels
	scale  float64 // pixels per twip
	width  int     // text width in twips
	first  int     // first line indent, twips from the left edge
	left   int     // other lines
	right  int     // right indent
	cm     bool

	drag  int // 0 none, 1 first, 2 hanging, 3 left (both), 4 right
	dragX int32
	start [3]int

	OnIndent func(first, left, right int)
}

func newRuler(parent ui.Parent) *Ruler {
	r := &Ruler{scale: 96.0 / 1440}
	r.ctl = ui.NewControl(parent, ui.OptsControl().
		ClassName("Word4WinRuler").
		ClassCursor(win.HCURSOR(loadCursor(idcArrow))).
		Style(co.WS_CHILD|co.WS_VISIBLE).
		Size(400, 24))
	on := r.ctl.On()
	on.WmEraseBkgnd(func(_ ui.WmEraseBkgnd) int { return 1 })
	on.WmPaint(r.paint)
	on.WmLButtonDown(func(p ui.WmMouse) {
		pt := p.Pos()
		r.drag = r.hit(pt)
		if r.drag != 0 {
			r.dragX = pt.X
			r.start = [3]int{r.first, r.left, r.right}
			procSetCapture.Call(uintptr(r.ctl.Hwnd()))
		}
	})
	on.WmMouseMove(func(p ui.WmMouse) {
		if r.drag == 0 {
			return
		}
		d := r.snap(float64(p.Pos().X-r.dragX) / r.scale)
		first, left, right := r.start[0], r.start[1], r.start[2]
		switch r.drag {
		case 1:
			first = max(0, first+d)
		case 2:
			left = max(0, left+d)
		case 3:
			d = max(d, -min(first, left))
			first, left = first+d, left+d
		case 4:
			right = right - d
		}
		right = min(right, r.width-max(first, left)-144)
		right = max(right, -1440)
		r.first, r.left, r.right = first, left, right
		invalidate(r.ctl.Hwnd())
	})
	on.WmLButtonUp(func(_ ui.WmMouse) {
		if r.drag != 0 {
			procReleaseCapture.Call()
			r.drag = 0
			if r.OnIndent != nil && [3]int{r.first, r.left, r.right} != r.start {
				r.OnIndent(r.first, r.left, r.right)
			}
		}
	})
	return r
}

func (r *Ruler) Hwnd() win.HWND { return r.ctl.Hwnd() }
func (r *Ruler) Height() int32  { return px(26) }

// snap rounds twips to the ruler's smallest marks: 1/16 inch or 1/4 cm.
func (r *Ruler) snap(tw float64) int {
	step := 90.0
	if r.cm {
		step = 1440 / 2.54 / 4
	}
	return int(math.Round(tw/step) * step)
}

func (r *Ruler) x(tw int) int32 { return r.origin + int32(math.Round(float64(tw)*r.scale)) }

func (r *Ruler) hit(pt win.POINT) int {
	rc, _ := r.ctl.Hwnd().GetClientRect()
	mid := rc.Bottom / 2
	near := func(x int32) bool { return pt.X >= x-px(6) && pt.X <= x+px(6) }
	switch {
	case near(r.x(r.width-r.right)) && pt.Y >= mid:
		return 4
	case near(r.x(r.first)) && pt.Y < mid:
		return 1
	case near(r.x(r.left)) && pt.Y >= mid && pt.Y < rc.Bottom-px(6):
		return 2
	case near(r.x(r.left)) && pt.Y >= rc.Bottom-px(6):
		return 3
	}
	return 0
}

func (r *Ruler) paint() {
	h := r.ctl.Hwnd()
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
	top, bottom := px(5), rc.Bottom-px(5)
	page := win.RECT{Left: r.x(0), Top: top, Right: r.x(r.width), Bottom: bottom}
	fillRect(hdc, &page, colors.page)
	old, _, _ := procSelectObject.Call(hdc, uiFont(-15, 0))
	// Marks: inches in eighths, or centimetres in quarters.
	unit, parts := 1440.0, 8
	if r.cm {
		unit, parts = 1440/2.54, 4
	}
	mid := (top + bottom) / 2
	for i := 0; ; i++ {
		tw := float64(i) * unit / float64(parts)
		x := r.x(int(math.Round(tw)))
		if x > rc.Right || tw > float64(r.width)+1 {
			break
		}
		if i == 0 {
			continue
		}
		switch {
		case i%parts == 0:
			label := fmt.Sprint(i / parts)
			drawText(hdc, label, win.RECT{Left: x - px(10), Top: top, Right: x + px(10), Bottom: bottom}, dtCenter|dtVCenter|dtSingleLine, colors.textDim)
		case parts == 8 && i%4 == 0 || parts == 4 && i%2 == 0:
			line(hdc, x, mid-px(3), x, mid+px(3), colors.textDim)
		default:
			line(hdc, x, mid-px(1), x, mid+px(1), colors.textDim)
		}
	}
	procSelectObject.Call(hdc, old)
	line(hdc, 0, rc.Bottom-1, rc.Right, rc.Bottom-1, colors.barLine)

	// Indent markers.
	marker := func(x int32, down bool) {
		s := px(5)
		var pts [3]win.POINT
		if down {
			pts = [3]win.POINT{{X: x - s, Y: top - px(1)}, {X: x + s, Y: top - px(1)}, {X: x, Y: top + s}}
		} else {
			pts = [3]win.POINT{{X: x - s, Y: bottom - px(1)}, {X: x + s, Y: bottom - px(1)}, {X: x, Y: bottom - px(1) - s}}
		}
		polygon(hdc, pts[:], colors.bar, colors.text)
	}
	marker(r.x(r.first), true)
	marker(r.x(r.left), false)
	box := win.RECT{Left: r.x(r.left) - px(5), Top: bottom, Right: r.x(r.left) + px(5) + 1, Bottom: bottom + px(4)}
	fillRect(hdc, &box, colors.text)
	inner := win.RECT{Left: box.Left + 1, Top: box.Top + 1, Right: box.Right - 1, Bottom: box.Bottom - 1}
	fillRect(hdc, &inner, colors.bar)
	marker(r.x(r.width-r.right), false)
	buf.blit(uintptr(hdcW), 0, 0)
}

func polygon(hdc uintptr, pts []win.POINT, fill, border uint32) {
	pen, _, _ := procCreatePen.Call(0, 1, uintptr(border))
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	oldBrush, _, _ := procSelectObject.Call(hdc, solidBrush(fill))
	procPolygon.Call(hdc, uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
	procSelectObject.Call(hdc, oldPen)
	procSelectObject.Call(hdc, oldBrush)
	procDeleteObject.Call(pen)
}
