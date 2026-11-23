//go:build windows

package ui

// Page setup, printing and the drawing of laid out pages with GDI. The
// pages come from internal/layout, the same ones the PDF is made of, so the
// print preview, the printout and the PDF look the same. Text is drawn
// with the advances of the layout (ExtTextOut with a dx array), so GDI's
// own measurements do not move it.

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/mevdschee/word4win/internal/doc"
	"github.com/mevdschee/word4win/internal/layout"
	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
)

type printState struct {
	devMode, devNames uintptr // the chosen printer and paper, kept while the app runs
}

type pageSetupDlg struct {
	size      uint32
	owner     uintptr
	devMode   uintptr
	devNames  uintptr
	flags     uint32
	paperSize win.POINT
	minMargin win.RECT
	margin    win.RECT
	instance  uintptr
	custData  uintptr
	setupHook uintptr
	paintHook uintptr
	template  *uint16
	hTemplate uintptr
}

type printDlg struct {
	size                                       uint32
	owner                                      uintptr
	devMode                                    uintptr
	devNames                                   uintptr
	hdc                                        uintptr
	flags                                      uint32
	fromPage, toPage, minPage, maxPage, copies uint16
	instance                                   uintptr
	custData                                   uintptr
	printHook, setupHook                       uintptr
	printTemplate, setupTemplate               *uint16
	hPrintTemplate, hSetupTemplate             uintptr
}

const (
	capHorzRes  = 8
	capVertRes  = 10
	capLogPxX   = 88
	capLogPxY   = 90
	capPhysW    = 110
	capPhysH    = 111
	capPhysOffX = 112
	capPhysOffY = 113

	idScaleEdit = 0x7001
)

var (
	pageSetupApp    *App
	pageSetupHookCb = syscall.NewCallback(pageSetupHook)
)

// pageSetupHook adds a Scale box to the standard Page Setup dialog.
func pageSetupHook(hwnd, msg, wParam, lParam uintptr) uintptr {
	a := pageSetupApp
	dlg := win.HWND(hwnd)
	switch co.WM(msg) {
	case co.WM_INITDIALOG:
		font, _ := dlg.SendMessage(co.WM_GETFONT, 0, 0)
		unit := win.RECT{Right: 4, Bottom: 8}
		dlg.MapDialogRect(&unit)
		ux, uy := unit.Right, unit.Bottom
		grow := uy * 3
		wr, _ := dlg.GetWindowRect()
		var lowest int32
		for _, c := range dlg.EnumChildWindows() {
			r, _ := c.GetWindowRect()
			dlg.ScreenToClientRc(&r)
			if id, _ := c.GetDlgCtrlID(); id == 1 || id == 2 || id == 0x0402 { // OK, Cancel, Printer...
				c.SetWindowPos(0, win.POINT{X: r.Left, Y: r.Top + grow}, win.SIZE{}, co.SWP_NOSIZE|co.SWP_NOZORDER)
			} else if p, _ := c.GetParent(); p == dlg {
				lowest = max(lowest, r.Bottom)
			}
		}
		dlg.SetWindowPos(0, win.POINT{}, win.SIZE{Cx: wr.Right - wr.Left, Cy: wr.Bottom - wr.Top + grow}, co.SWP_NOMOVE|co.SWP_NOZORDER)
		hInst, _ := dlg.HInstance()
		add := func(class, text string, style, exStyle uintptr, x, y, w, h int32, id uintptr) {
			c, _, _ := procCreateWindowExW.Call(exStyle, uintptr(unsafe.Pointer(utf16Ptr(class))),
				uintptr(unsafe.Pointer(utf16Ptr(text))), style|uintptr(co.WS_CHILD|co.WS_VISIBLE),
				uintptr(x), uintptr(y), uintptr(w), uintptr(h), hwnd, id, uintptr(hInst), 0)
			win.HWND(c).SendMessage(co.WM_SETFONT, win.WPARAM(font), 1)
		}
		left := ux * 2
		y := lowest + uy
		add("STATIC", "&Scale (%):", 0, 0, left, y+uy/4, ux*14, uy, 0)
		add("EDIT", itoa(a.cfg.PrintScale), uintptr(co.WS_TABSTOP)|0x2000, 0x0200, left+ux*14, y, ux*10, uy*3/2, idScaleEdit) // ES_NUMBER
	case co.WM_COMMAND:
		if uint16(wParam) == 1 {
			if h, _ := dlg.GetDlgItem(idScaleEdit); h != 0 {
				if v := atoi(windowText(h)); v >= 10 && v <= 400 {
					a.cfg.PrintScale = v
				}
			}
		}
	}
	return 0
}

func itoa(n int) string {
	return string(appendInt(nil, n))
}

func appendInt(b []byte, n int) []byte {
	if n < 0 {
		b = append(b, '-')
		n = -n
	}
	var d [20]byte
	i := len(d)
	for {
		i--
		d[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	return append(b, d[i:]...)
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// DEVMODEW: the offsets of fields after the 32 character device name.
const (
	devModeFields      = 72 // dmFields
	devModeOrientation = 76 // dmOrientation
	dmOrientationField = 0x1
	dmorientPortrait   = 1
	dmorientLandscape  = 2
)

// lockDevMode locks a DEVMODE handle and returns its memory; nil if there
// is none. GlobalUnlock ends it.
func lockDevMode(h uintptr) unsafe.Pointer {
	if h == 0 {
		return nil
	}
	p, _, _ := procGlobalLock.Call(h)
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

// devModeLandscape reads the orientation of printer settings; ok is false
// when they have none.
func devModeLandscape(h uintptr) (landscape, ok bool) {
	p := lockDevMode(h)
	if p == nil {
		return false, false
	}
	defer procGlobalUnlock.Call(h)
	if *(*uint32)(unsafe.Add(p, devModeFields))&dmOrientationField == 0 {
		return false, false
	}
	return *(*int16)(unsafe.Add(p, devModeOrientation)) == dmorientLandscape, true
}

// setDevModeLandscape sets the orientation of printer settings.
func setDevModeLandscape(h uintptr, landscape bool) {
	p := lockDevMode(h)
	if p == nil {
		return
	}
	defer procGlobalUnlock.Call(h)
	*(*uint32)(unsafe.Add(p, devModeFields)) |= dmOrientationField
	o := int16(dmorientPortrait)
	if landscape {
		o = dmorientLandscape
	}
	*(*int16)(unsafe.Add(p, devModeOrientation)) = o
}

// landscape tells whether the page setup is landscape.
func (a *App) landscape() bool {
	return a.cfg.PaperWidth > a.cfg.PaperHeight
}

// syncPrinter gives the printer settings, which the dialogs and printing
// use, the orientation of the page setup. Without settings of this session
// (after a restart) it takes those of the default printer: their own
// orientation would show portrait in the dialogs and print a landscape
// page on portrait paper.
func (a *App) syncPrinter() {
	if a.cfg.PaperWidth <= 0 || a.cfg.PaperHeight <= 0 {
		return // the paper follows Windows
	}
	if a.print.devMode == 0 {
		psd := pageSetupDlg{owner: uintptr(a.wnd.Hwnd()), flags: 0x0400} // PSD_RETURNDEFAULT
		psd.size = uint32(unsafe.Sizeof(psd))
		if r, _, _ := procPageSetupDlgW.Call(uintptr(unsafe.Pointer(&psd))); r == 0 {
			return
		}
		a.print.devMode, a.print.devNames = psd.devMode, psd.devNames
	}
	setDevModeLandscape(a.print.devMode, a.landscape())
}

func (a *App) pageSetup() {
	// Margins in thousandths of an inch or hundredths of a millimetre,
	// as the locale measures.
	metric := !localeIsUS()
	toDlg := func(pt float64) int32 {
		if metric {
			return int32(math.Round(pt / 72 * 2540))
		}
		return int32(math.Round(pt / 72 * 1000))
	}
	fromDlg := func(v int32) float64 {
		if metric {
			return float64(v) / 2540 * 72
		}
		return float64(v) / 1000 * 72
	}
	unitsFlag := uint32(0x0004) // PSD_INTHOUSANDTHSOFINCHES
	if metric {
		unitsFlag = 0x0008 // PSD_INHUNDREDTHSOFMILLIMETERS
	}
	a.syncPrinter()
	psd := pageSetupDlg{
		owner: uintptr(a.wnd.Hwnd()), devMode: a.print.devMode, devNames: a.print.devNames,
		flags: 0x0002 | unitsFlag | 0x2000, // PSD_MARGINS | units | PSD_ENABLEPAGESETUPHOOK
		margin: win.RECT{Left: toDlg(a.cfg.MarginLeft), Top: toDlg(a.cfg.MarginTop),
			Right: toDlg(a.cfg.MarginRight), Bottom: toDlg(a.cfg.MarginBottom)},
		setupHook: pageSetupHookCb,
	}
	psd.size = uint32(unsafe.Sizeof(psd))
	pageSetupApp = a
	if r, _, _ := procPageSetupDlgW.Call(uintptr(unsafe.Pointer(&psd))); r == 0 {
		if code, _, _ := procCommDlgExtendedErr.Call(); code != 0 {
			a.wnd.Hwnd().MessageBox("The page setup is not available. Is a printer installed?", appName, co.MB_ICONEXCLAMATION)
		}
		return
	}
	a.print.devMode, a.print.devNames = psd.devMode, psd.devNames
	a.cfg.MarginLeft, a.cfg.MarginTop = fromDlg(psd.margin.Left), fromDlg(psd.margin.Top)
	a.cfg.MarginRight, a.cfg.MarginBottom = fromDlg(psd.margin.Right), fromDlg(psd.margin.Bottom)
	if psd.paperSize.X > 0 && psd.paperSize.Y > 0 {
		// The orientation chosen is that of the printer settings; the
		// paper size does not follow it with every printer driver.
		w, h := fromDlg(psd.paperSize.X), fromDlg(psd.paperSize.Y)
		if landscape, ok := devModeLandscape(psd.devMode); ok && landscape != (w > h) {
			w, h = h, w
		}
		a.cfg.PaperWidth, a.cfg.PaperHeight = w, h
	}
	a.saveSettings()
	a.applyWrap()
	a.updateRuler()
}

func (a *App) printDialog() {
	pages := layout.Layout(a.model(), a.layoutSettings())
	a.syncPrinter()
	pd := printDlg{
		owner: uintptr(a.wnd.Hwnd()), devMode: a.print.devMode, devNames: a.print.devNames,
		flags:   0x0100 | 0x0004 | 0x00040000, // PD_RETURNDC | PD_NOSELECTION | PD_USEDEVMODECOPIESANDCOLLATE
		copies:  1,
		minPage: 1, maxPage: uint16(len(pages)), fromPage: 1, toPage: uint16(len(pages)),
	}
	pd.size = uint32(unsafe.Sizeof(pd))
	if r, _, _ := procPrintDlgW.Call(uintptr(unsafe.Pointer(&pd))); r == 0 {
		if code, _, _ := procCommDlgExtendedErr.Call(); code != 0 {
			a.wnd.Hwnd().MessageBox("Cannot print. Is a printer installed?", appName, co.MB_ICONEXCLAMATION)
		}
		return
	}
	a.print.devMode, a.print.devNames = pd.devMode, pd.devNames
	from, to := 1, len(pages)
	if pd.flags&0x0002 != 0 { // PD_PAGENUMS
		from, to = int(pd.fromPage), int(pd.toPage)
	}
	a.printPages(pd.hdc, pages, from, to)
}

// printNow prints on the default printer without asking.
func (a *App) printNow() {
	// PD_RETURNDEFAULT takes no printer settings: it returns those of the
	// default printer.
	pd := printDlg{owner: uintptr(a.wnd.Hwnd()), flags: 0x0100 | 0x0400} // PD_RETURNDC | PD_RETURNDEFAULT
	pd.size = uint32(unsafe.Sizeof(pd))
	if r, _, _ := procPrintDlgW.Call(uintptr(unsafe.Pointer(&pd))); r == 0 || pd.hdc == 0 {
		a.wnd.Hwnd().MessageBox("Cannot print: there is no default printer.", appName, co.MB_ICONEXCLAMATION)
		return
	}
	if a.print.devMode == 0 {
		a.print.devMode, a.print.devNames = pd.devMode, pd.devNames
	} else {
		procGlobalFree.Call(pd.devMode)
		procGlobalFree.Call(pd.devNames)
	}
	pages := layout.Layout(a.model(), a.layoutSettings())
	a.printPages(pd.hdc, pages, 1, len(pages))
}

func (a *App) printPages(hdc uintptr, pages []*layout.Page, from, to int) {
	if hdc == 0 {
		return
	}
	defer procDeleteDC.Call(hdc)
	restore := waitCursor()
	defer restore()
	// Print in the orientation of the pages, whatever the printer's is.
	if len(pages) > 0 && a.cfg.PaperWidth > 0 && a.print.devMode != 0 {
		setDevModeLandscape(a.print.devMode, pages[0].Width > pages[0].Height)
		if p := lockDevMode(a.print.devMode); p != nil {
			if r, _, _ := procResetDCW.Call(hdc, uintptr(p)); r != 0 {
				hdc = r
			}
			procGlobalUnlock.Call(a.print.devMode)
		}
	}
	devCap := func(i uintptr) float64 {
		r, _, _ := procGetDeviceCaps.Call(hdc, i)
		return float64(int32(r))
	}
	dpiX := devCap(capLogPxX)
	offX, offY := devCap(capPhysOffX), devCap(capPhysOffY)
	di := struct {
		size     int32
		name     *uint16
		output   *uint16
		dataType *uint16
		fwType   uint32
	}{name: utf16Ptr(a.name())}
	di.size = int32(unsafe.Sizeof(di))
	if r, _, _ := procStartDocW.Call(hdc, uintptr(unsafe.Pointer(&di))); int32(r) <= 0 {
		a.wnd.Hwnd().MessageBox("The document could not be printed.", appName, co.MB_ICONEXCLAMATION)
		return
	}
	for i := max(1, from); i <= min(to, len(pages)); i++ {
		if r, _, _ := procStartPage.Call(hdc); int32(r) <= 0 {
			procAbortDoc.Call(hdc)
			return
		}
		renderPage(hdc, pages[i-1], dpiX/72, -offX, -offY)
		procEndPage.Call(hdc)
	}
	procEndDoc.Call(hdc)
}

// ---- drawing pages ----

type gdiFontKey struct {
	face         string
	bold, italic bool
	height       int32
}

// renderPage draws a page: s is device pixels per point and (ox, oy) the
// device position of the page's top left corner.
func renderPage(hdc uintptr, p *layout.Page, s, ox, oy float64) {
	fonts := map[gdiFontKey]uintptr{}
	defer func() {
		for _, f := range fonts {
			deleteObject(f)
		}
	}()
	X := func(v float64) int32 { return int32(math.Round(ox + v*s)) }
	Y := func(v float64) int32 { return int32(math.Round(oy + v*s)) }
	procSetBkMode.Call(hdc, 1)
	procSetTextAlign.Call(hdc, 0x0018) // TA_BASELINE | TA_LEFT
	oldFont, _, _ := procSelectObject.Call(hdc, uintptr(0))
	_ = oldFont
	var saved uintptr
	for _, it := range p.Items {
		switch v := it.(type) {
		case *layout.Rect:
			rc := win.RECT{Left: X(v.X), Top: Y(v.Y), Right: X(v.X + v.W), Bottom: Y(v.Y + v.H)}
			fillRect(hdc, &rc, colorOf(v.Fill))
		case *layout.Line:
			w := math.Max(1, math.Round(v.Width*s))
			var rc win.RECT
			if v.Y1 == v.Y2 {
				y := Y(v.Y1) - int32(w/2)
				rc = win.RECT{Left: X(v.X1), Top: y, Right: X(v.X2), Bottom: y + int32(w)}
			} else {
				x := X(v.X1) - int32(w/2)
				rc = win.RECT{Left: x, Top: Y(v.Y1), Right: x + int32(w), Bottom: Y(v.Y2)}
			}
			if rc.Right == rc.Left {
				rc.Right++
			}
			if rc.Bottom == rc.Top {
				rc.Bottom++
			}
			fillRect(hdc, &rc, colorOf(v.Color))
		case *layout.Picture:
			drawPicture(hdc, v.Image, X(v.X), Y(v.Y), X(v.X+v.W)-X(v.X), Y(v.Y+v.H)-Y(v.Y))
		case *layout.Text:
			if v.Face.Font == nil {
				continue
			}
			key := gdiFontKey{face: v.Face.Font.Family, bold: v.Face.Font.Bold || v.Face.SynthBold,
				italic: v.Face.Font.Italic || v.Face.SynthItalic, height: int32(math.Round(v.Size * s))}
			if key.height < 1 {
				continue
			}
			f, ok := fonts[key]
			if !ok {
				lf := logFont{Height: -key.height, Weight: 400, Quality: 5} // CLEARTYPE_QUALITY
				if key.bold {
					lf.Weight = 700
				}
				if key.italic {
					lf.Italic = 1
				}
				lf.CharSet = 1
				lf.setFace(key.face)
				f = createFont(&lf)
				fonts[key] = f
			}
			old, _, _ := procSelectObject.Call(hdc, f)
			if saved == 0 {
				saved = old
			}
			procSetTextColor.Call(hdc, uintptr(colorOf(v.Color)))
			u := utf16.Encode([]rune(v.Text))
			dx := make([]int32, len(u))
			cum, prev := 0.0, X(v.X)
			k := 0
			for i, r := range []rune(v.Text) {
				cum += v.Advances[i]
				next := X(v.X + cum)
				dx[k] = next - prev
				prev = next
				k++
				if r >= 0x10000 { // the second half of a surrogate pair
					dx[k] = 0
					k++
				}
			}
			if len(u) > 0 {
				procExtTextOutW.Call(hdc, uintptr(X(v.X)), uintptr(Y(v.Y)), 0, 0,
					uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), uintptr(unsafe.Pointer(&dx[0])))
			}
		}
	}
	if saved != 0 {
		procSelectObject.Call(hdc, saved)
	}
}

func colorOf(c doc.Color) uint32 { return colorref(c.R, c.G, c.B) }

// picture bitmaps, decoded once and composed on white.
type dib struct {
	bmp  uintptr
	w, h int
	pix  []byte
}

var pictures = map[*doc.Image]*dib{}

func pictureDIB(img *doc.Image) *dib {
	if d, ok := pictures[img]; ok {
		return d
	}
	pictures[img] = nil
	src, _, err := image.Decode(bytes.NewReader(img.Data))
	if err != nil {
		return nil
	}
	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Over)
	d := &dib{w: b.Dx(), h: b.Dy()}
	d.pix = make([]byte, d.w*d.h*4)
	for i := 0; i < d.w*d.h; i++ {
		d.pix[i*4], d.pix[i*4+1], d.pix[i*4+2], d.pix[i*4+3] = rgba.Pix[i*4+2], rgba.Pix[i*4+1], rgba.Pix[i*4], 0xFF
	}
	pictures[img] = d
	return d
}

func drawPicture(hdc uintptr, img *doc.Image, x, y, w, h int32) {
	d := pictureDIB(img)
	if d == nil || w <= 0 || h <= 0 {
		return
	}
	bih := bitmapInfoHeader{width: int32(d.w), height: -int32(d.h), planes: 1, bitCount: 32}
	bih.size = uint32(unsafe.Sizeof(bih))
	procSetStretchBltMode.Call(hdc, 4) // HALFTONE
	procSetBrushOrgEx.Call(hdc, 0, 0, 0)
	procStretchDIBits.Call(hdc, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0, 0, uintptr(d.w), uintptr(d.h),
		uintptr(unsafe.Pointer(&d.pix[0])), uintptr(unsafe.Pointer(&bih)), 0, srcCopy)
}
