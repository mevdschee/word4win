//go:build windows

package ui

import (
	"image/color"
	"syscall"
	"unsafe"

	"github.com/mevdschee/word4win/internal/icons"
	"github.com/rodrigocfd/windigo/win"
)

// dpiScale converts 96 dpi pixels to the screen's pixels.
func px(v int) int32 { return int32(mulDiv(v, screenDpi(), 96)) }

var cachedDpi int

func screenDpi() int {
	if cachedDpi == 0 {
		hdc, _, _ := procGetDC.Call(0)
		r, _, _ := procGetDeviceCaps.Call(hdc, capLogPxY)
		procReleaseDC.Call(0, hdc)
		cachedDpi = int(r)
		if cachedDpi <= 0 {
			cachedDpi = 96
		}
	}
	return cachedDpi
}

// iconBitmap is a rendered icon in a 32-bit DIB with premultiplied alpha,
// ready for AlphaBlend.
type iconBitmap struct {
	bmp  uintptr
	size int
}

type iconKey struct {
	name  string
	size  int
	color uint32
}

var iconCache = map[iconKey]iconBitmap{}

type bitmapInfoHeader struct {
	size                       uint32
	width, height              int32
	planes, bitCount           uint16
	compression, sizeImage     uint32
	xPelsPerMeter, yPelsPerMtr int32
	clrUsed, clrImportant      uint32
}

// newDIB makes a top-down 32-bit DIB section and returns its pixels.
func newDIB(w, h int) (uintptr, []byte) {
	bih := bitmapInfoHeader{width: int32(w), height: -int32(h), planes: 1, bitCount: 32}
	bih.size = uint32(unsafe.Sizeof(bih))
	var bits unsafe.Pointer
	hbmp, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bih)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbmp == 0 || bits == nil {
		return 0, nil
	}
	return hbmp, unsafe.Slice((*byte)(bits), w*h*4)
}

func getIcon(name string, size int, c uint32) iconBitmap {
	key := iconKey{name, size, c}
	if ib, ok := iconCache[key]; ok {
		return ib
	}
	img, err := icons.Render(name, size, color.RGBA{uint8(c), uint8(c >> 8), uint8(c >> 16), 0xFF})
	ib := iconBitmap{size: size}
	if err == nil {
		bmp, pix := newDIB(size, size)
		if bmp != 0 {
			for i := 0; i < size*size; i++ { // RGBA to BGRA, both premultiplied
				pix[i*4+0] = img.Pix[i*4+2]
				pix[i*4+1] = img.Pix[i*4+1]
				pix[i*4+2] = img.Pix[i*4+0]
				pix[i*4+3] = img.Pix[i*4+3]
			}
			ib.bmp = bmp
		}
	}
	iconCache[key] = ib
	return ib
}

// drawIcon draws a Fluent icon in a colour.
func drawIcon(hdc uintptr, name string, x, y int32, size int, c uint32) {
	ib := getIcon(name, size, c)
	if ib.bmp == 0 {
		return
	}
	mem, _, _ := procCreateCompatibleDC.Call(hdc)
	old, _, _ := procSelectObject.Call(mem, ib.bmp)
	blend := uintptr(0x01FF0000) // AC_SRC_OVER, alpha 255, AC_SRC_ALPHA
	procGdiAlphaBlend.Call(hdc, uintptr(x), uintptr(y), uintptr(size), uintptr(size), mem, 0, 0, uintptr(size), uintptr(size), blend)
	procSelectObject.Call(mem, old)
	procDeleteDC.Call(mem)
}

// buffer is a memory device context for flicker-free painting.
type buffer struct {
	hdc, bmp, old uintptr
	w, h          int32
}

func newBuffer(hdc uintptr, w, h int32) *buffer {
	b := &buffer{w: max(1, w), h: max(1, h)}
	b.hdc, _, _ = procCreateCompatibleDC.Call(hdc)
	b.bmp, _, _ = procCreateCompatibleBitmap.Call(hdc, uintptr(b.w), uintptr(b.h))
	b.old, _, _ = procSelectObject.Call(b.hdc, b.bmp)
	return b
}

func (b *buffer) blit(hdc uintptr, x, y int32) {
	procBitBlt.Call(hdc, uintptr(x), uintptr(y), uintptr(b.w), uintptr(b.h), b.hdc, 0, 0, srcCopy)
	procSelectObject.Call(b.hdc, b.old)
	procDeleteObject.Call(b.bmp)
	procDeleteDC.Call(b.hdc)
}

func drawText(hdc uintptr, s string, rc win.RECT, flags uintptr, c uint32) {
	if s == "" {
		return
	}
	u, _ := syscall.UTF16FromString(s)
	procSetBkMode.Call(hdc, 1)
	procSetTextColor.Call(hdc, uintptr(c))
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&u[0])), ^uintptr(0), uintptr(unsafe.Pointer(&rc)), flags|0x0800) // DT_NOPREFIX
}

const (
	dtLeft        = 0x0000
	dtCenter      = 0x0001
	dtRight       = 0x0002
	dtVCenter     = 0x0004
	dtSingleLine  = 0x0020
	dtEndEllipsis = 0x8000
	dtWordBreak   = 0x0010
	dtCalcRect    = 0x0400
)

// line draws a line of 1 pixel.
func line(hdc uintptr, x1, y1, x2, y2 int32, c uint32) {
	pen, _, _ := procCreatePen.Call(0, 1, uintptr(c))
	old, _, _ := procSelectObject.Call(hdc, pen)
	procMoveToEx.Call(hdc, uintptr(x1), uintptr(y1), 0)
	procLineTo.Call(hdc, uintptr(x2), uintptr(y2))
	procSelectObject.Call(hdc, old)
	procDeleteObject.Call(pen)
}

// roundRect fills a rounded rectangle, with an optional border.
func roundRect(hdc uintptr, rc win.RECT, radius int32, fill, border uint32) {
	brush := solidBrush(fill)
	penColor := border
	if border == 0xFFFFFFFF {
		penColor = fill
	}
	pen, _, _ := procCreatePen.Call(0, 1, uintptr(penColor))
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	procRoundRect.Call(hdc, uintptr(rc.Left), uintptr(rc.Top), uintptr(rc.Right), uintptr(rc.Bottom), uintptr(radius), uintptr(radius))
	procSelectObject.Call(hdc, oldPen)
	procSelectObject.Call(hdc, oldBrush)
	procDeleteObject.Call(pen)
}

// uiFont is the message font of Windows (Segoe UI), optionally bigger or
// bolder. Fonts are cached.
var uiFonts = map[[2]int]uintptr{}

func uiFont(grow, weight int) uintptr {
	key := [2]int{grow, weight}
	if f, ok := uiFonts[key]; ok {
		return f
	}
	msg, _, _ := systemFonts()
	if grow != 0 {
		msg.Height = msg.Height * int32(100+grow) / 100
	}
	if weight != 0 {
		msg.Weight = int32(weight)
	}
	f := createFont(&msg)
	uiFonts[key] = f
	return f
}

func textSize(hdc uintptr, font uintptr, s string) (int32, int32) {
	old, _, _ := procSelectObject.Call(hdc, font)
	defer procSelectObject.Call(hdc, old)
	u := syscall.StringToUTF16(s)
	var sz size
	procGetTextExtentPoint32W.Call(hdc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&sz)))
	return sz.cx, sz.cy
}

func invalidate(h win.HWND) { procInvalidateRect.Call(uintptr(h), 0, 0) }

// trackLeave asks for WM_MOUSELEAVE.
func trackLeave(h win.HWND) {
	tme := struct {
		size, flags uint32
		hwnd        uintptr
		hover       uint32
	}{flags: 0x0002, hwnd: uintptr(h)} // TME_LEAVE
	tme.size = uint32(unsafe.Sizeof(tme))
	procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
}

func colorref(r, g, b uint8) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }

// swatchBitmap makes a small bitmap of a colour for a menu item.
func swatchBitmap(c uint32, w, h int, border uint32) uintptr {
	bmp, pix := newDIB(w, h)
	if bmp == 0 {
		return 0
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := c
			if x == 0 || y == 0 || x == w-1 || y == h-1 {
				v = border
			}
			i := (y*w + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = byte(v>>16), byte(v>>8), byte(v), 0xFF
		}
	}
	return bmp
}
