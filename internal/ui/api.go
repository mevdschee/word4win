//go:build windows

// Package ui is the word4win window: a WordPad look-alike made of a
// menu bar, toolbars, a ruler, a RichEdit control and a status bar.
package ui

import (
	"syscall"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
	"golang.org/x/sys/windows"
)

// Win32 functions that windigo does not wrap, or not in the form needed.
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	comdlg32 = windows.NewLazySystemDLL("comdlg32.dll")
	comctl32 = windows.NewLazySystemDLL("comctl32.dll")
	uxtheme  = windows.NewLazySystemDLL("uxtheme.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	ntdll    = windows.NewLazySystemDLL("ntdll.dll")

	procAppendMenuW           = user32.NewProc("AppendMenuW")
	procEnableMenuItem        = user32.NewProc("EnableMenuItem")
	procSetMenuInfo           = user32.NewProc("SetMenuInfo")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procShowScrollBar         = user32.NewProc("ShowScrollBar")
	procEnableScrollBar       = user32.NewProc("EnableScrollBar")
	procGetSysColor           = user32.NewProc("GetSysColor")
	procLoadImageW            = user32.NewProc("LoadImageW")
	procDrawIconEx            = user32.NewProc("DrawIconEx")
	procDestroyIcon           = user32.NewProc("DestroyIcon")
	procMessageBeep           = user32.NewProc("MessageBeep")
	procGetMenuItemRect       = user32.NewProc("GetMenuItemRect")
	procGetMenuBarInfo        = user32.NewProc("GetMenuBarInfo")
	procGetMenuItemInfoW      = user32.NewProc("GetMenuItemInfoW")
	procDrawTextW             = user32.NewProc("DrawTextW")
	procFillRect              = user32.NewProc("FillRect")
	procIsWindowEnabled       = user32.NewProc("IsWindowEnabled")
	procIsWindowVisible       = user32.NewProc("IsWindowVisible")
	procGetWindowTextW        = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW  = user32.NewProc("GetWindowTextLengthW")
	procSetWindowTextW        = user32.NewProc("SetWindowTextW")
	procCreateWindowExW       = user32.NewProc("CreateWindowExW")
	procRegisterClassExW      = user32.NewProc("RegisterClassExW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procGetDlgItem            = user32.NewProc("GetDlgItem")
	procIsDlgButtonChecked    = user32.NewProc("IsDlgButtonChecked")
	procCheckDlgButton        = user32.NewProc("CheckDlgButton")
	procCheckRadioButton      = user32.NewProc("CheckRadioButton")
	procGetWindowRect         = user32.NewProc("GetWindowRect")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procWaitMessage           = user32.NewProc("WaitMessage")
	procSetCursor             = user32.NewProc("SetCursor")
	procLoadCursorW           = user32.NewProc("LoadCursorW")
	procRedrawWindow          = user32.NewProc("RedrawWindow")
	procRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	procTrackMouseEvent       = user32.NewProc("TrackMouseEvent")
	procInvalidateRect        = user32.NewProc("InvalidateRect")
	procSetMenuItemInfoW      = user32.NewProc("SetMenuItemInfoW")
	procInsertMenuItemW       = user32.NewProc("InsertMenuItemW")
	procGetDC                 = user32.NewProc("GetDC")
	procReleaseDC             = user32.NewProc("ReleaseDC")
	procIsWindow              = user32.NewProc("IsWindow")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procSetCapture            = user32.NewProc("SetCapture")
	procReleaseCapture        = user32.NewProc("ReleaseCapture")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procScreenToClient        = user32.NewProc("ScreenToClient")
	procClientToScreen        = user32.NewProc("ClientToScreen")
	procMoveWindow            = user32.NewProc("MoveWindow")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procGetFocus              = user32.NewProc("GetFocus")
	procSetFocus              = user32.NewProc("SetFocus")
	procShowWindow            = user32.NewProc("ShowWindow")
	procEnableWindow          = user32.NewProc("EnableWindow")
	procGetScrollInfo         = user32.NewProc("GetScrollInfo")
	procSetScrollInfo         = user32.NewProc("SetScrollInfo")
	procScrollWindow          = user32.NewProc("ScrollWindow")
	procDrawFocusRect         = user32.NewProc("DrawFocusRect")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procGetDpiForWindow       = user32.NewProc("GetDpiForWindow")
	procFrameRect             = user32.NewProc("FrameRect")
	procGetClassLongPtrW      = user32.NewProc("GetClassLongPtrW")
	procCallWindowProcW       = user32.NewProc("CallWindowProcW")

	procExtTextOutW            = gdi32.NewProc("ExtTextOutW")
	procGetTextExtentPoint32W  = gdi32.NewProc("GetTextExtentPoint32W")
	procCreateFontIndirectW    = gdi32.NewProc("CreateFontIndirectW")
	procGetTextMetricsW        = gdi32.NewProc("GetTextMetricsW")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procSetTextColor           = gdi32.NewProc("SetTextColor")
	procSetBkColor             = gdi32.NewProc("SetBkColor")
	procSetBkMode              = gdi32.NewProc("SetBkMode")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procGetDeviceCaps          = gdi32.NewProc("GetDeviceCaps")
	procStartDocW              = gdi32.NewProc("StartDocW")
	procEndDoc                 = gdi32.NewProc("EndDoc")
	procAbortDoc               = gdi32.NewProc("AbortDoc")
	procStartPage              = gdi32.NewProc("StartPage")
	procEndPage                = gdi32.NewProc("EndPage")
	procGetObjectW             = gdi32.NewProc("GetObjectW")
	procGetStockObject         = gdi32.NewProc("GetStockObject")
	procCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	procStretchDIBits          = gdi32.NewProc("StretchDIBits")
	procSetStretchBltMode      = gdi32.NewProc("SetStretchBltMode")
	procSetBrushOrgEx          = gdi32.NewProc("SetBrushOrgEx")
	procCreatePen              = gdi32.NewProc("CreatePen")
	procMoveToEx               = gdi32.NewProc("MoveToEx")
	procLineTo                 = gdi32.NewProc("LineTo")
	procRectangle              = gdi32.NewProc("Rectangle")
	procPolygon                = gdi32.NewProc("Polygon")
	procSetGraphicsMode        = gdi32.NewProc("SetGraphicsMode")
	procSetWorldTransform      = gdi32.NewProc("SetWorldTransform")
	procModifyWorldTransform   = gdi32.NewProc("ModifyWorldTransform")
	procSaveDC                 = gdi32.NewProc("SaveDC")
	procRestoreDC              = gdi32.NewProc("RestoreDC")
	procIntersectClipRect      = gdi32.NewProc("IntersectClipRect")
	procSetTextAlign           = gdi32.NewProc("SetTextAlign")
	procEnumFontFamiliesExW    = gdi32.NewProc("EnumFontFamiliesExW")
	procAlphaBlend             = windows.NewLazySystemDLL("msimg32.dll").NewProc("AlphaBlend")
	procGdiAlphaBlend          = gdi32.NewProc("GdiAlphaBlend")
	procRoundRect              = gdi32.NewProc("RoundRect")
	procCreateRoundRectRgn     = gdi32.NewProc("CreateRoundRectRgn")
	procSetDCBrushColor        = gdi32.NewProc("SetDCBrushColor")
	procSetDCPenColor          = gdi32.NewProc("SetDCPenColor")

	procGlobalLock     = kernel32.NewProc("GlobalLock")
	procGlobalUnlock   = kernel32.NewProc("GlobalUnlock")
	procGlobalFree     = kernel32.NewProc("GlobalFree")
	procGetTimeFormatW = kernel32.NewProc("GetTimeFormatW")
	procGetDateFormatW = kernel32.NewProc("GetDateFormatW")
	procMulDiv         = kernel32.NewProc("MulDiv")
	procReplaceFileW   = kernel32.NewProc("ReplaceFileW")
	procLoadLibraryW   = kernel32.NewProc("LoadLibraryW")
	procGetLocaleInfoW = kernel32.NewProc("GetLocaleInfoW")

	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")

	procShellExecuteW   = shell32.NewProc("ShellExecuteW")
	procDragAcceptFiles = shell32.NewProc("DragAcceptFiles")

	procChooseFontW        = comdlg32.NewProc("ChooseFontW")
	procPrintDlgW          = comdlg32.NewProc("PrintDlgW")
	procPageSetupDlgW      = comdlg32.NewProc("PageSetupDlgW")
	procCommDlgExtendedErr = comdlg32.NewProc("CommDlgExtendedError")
	procFindTextW          = comdlg32.NewProc("FindTextW")
	procReplaceTextW       = comdlg32.NewProc("ReplaceTextW")
	procChooseColorW       = comdlg32.NewProc("ChooseColorW")

	procSetWindowSubclass = comctl32.NewProc("SetWindowSubclass")
	procDefSubclassProc   = comctl32.NewProc("DefSubclassProc")

	procSetWindowTheme   = uxtheme.NewProc("SetWindowTheme")
	procDwmSetWindowAttr = dwmapi.NewProc("DwmSetWindowAttribute")
	procWineGetVersion   = ntdll.NewProc("wine_get_version")
)

func isWine() bool { return procWineGetVersion.Find() == nil }

// logFont is LOGFONTW.
type logFont struct {
	Height, Width, Escapement, Orientation, Weight int32
	Italic, Underline, StrikeOut, CharSet          byte
	OutPrecision, ClipPrecision, Quality, Pitch    byte
	FaceName                                       [32]uint16
}

func (lf *logFont) face() string { return syscall.UTF16ToString(lf.FaceName[:]) }

func (lf *logFont) setFace(name string) {
	lf.FaceName = [32]uint16{}
	u, _ := syscall.UTF16FromString(name)
	copy(lf.FaceName[:31], u)
}

// textMetric is TEXTMETRICW.
type textMetric struct {
	Height, Ascent, Descent, InternalLeading, ExternalLeading int32
	AveCharWidth, MaxCharWidth, Weight, Overhang              int32
	DigitizedAspectX, DigitizedAspectY                        int32
	FirstChar, LastChar, DefaultChar, BreakChar               uint16
	Italic, Underlined, StruckOut, PitchAndFamily, CharSet    byte
}

type size struct{ cx, cy int32 }

const (
	etoOpaque  = 0x0002
	etoClipped = 0x0004

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfPopup     = 0x0010
	mfSeparator = 0x0800

	srcCopy = 0x00CC0020

	cfUnicodeText = 13

	wmMouseWheel       = 0x020A
	wmMouseHWheel      = 0x020E
	wmSettingChange    = 0x001A
	wmDpiChanged       = 0x02E0
	wmUser             = 0x0400
	spiGetWheelLines   = 0x0068
	spiGetCaretWidth   = 0x2006
	spiNonClientMetric = 0x0029
	spiGetHighContrast = 0x0042
)

func ptr[T any](p *T) unsafe.Pointer { return unsafe.Pointer(p) }

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func sysColor(index co.COLOR) uint32 {
	c, _, _ := procGetSysColor.Call(uintptr(index))
	return uint32(c)
}

// brushes caches one solid brush per colour; there are only a few.
var brushes = map[uint32]uintptr{}

func solidBrush(c uint32) uintptr {
	b, ok := brushes[c]
	if !ok {
		b, _, _ = procCreateSolidBrush.Call(uintptr(c))
		brushes[c] = b
	}
	return b
}

func fillRect(hdc uintptr, rc *win.RECT, color uint32) {
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(rc)), solidBrush(color))
}

func appendMenu(h win.HMENU, flags uintptr, id uintptr, text string) {
	var p *uint16
	if text != "" {
		p = utf16Ptr(text)
	}
	procAppendMenuW.Call(uintptr(h), flags, id, uintptr(unsafe.Pointer(p)))
}

func mulDiv(a, b, c int) int {
	r, _, _ := procMulDiv.Call(uintptr(a), uintptr(b), uintptr(c))
	return int(int32(r))
}

func keyDown(vk co.VK) bool {
	down, _ := win.GetKeyState(vk)
	return down
}

func beep() { procMessageBeep.Call(0xFFFFFFFF) }

func loadCursor(id uintptr) uintptr {
	h, _, _ := procLoadCursorW.Call(0, id)
	return h
}

const (
	idcArrow = 32512
	idcIBeam = 32513
	idcWait  = 32514
)

// waitCursor shows the hourglass until the returned function is called.
func waitCursor() func() {
	old, _, _ := procSetCursor.Call(loadCursor(idcWait))
	return func() { procSetCursor.Call(old) }
}

// shellOpen opens a file or URL with its default application.
func shellOpen(owner win.HWND, target string) {
	procShellExecuteW.Call(uintptr(owner), uintptr(unsafe.Pointer(utf16Ptr("open"))),
		uintptr(unsafe.Pointer(utf16Ptr(target))), 0, 0, 1) // SW_SHOWNORMAL
}

// nonClientMetrics is NONCLIENTMETRICSW.
type nonClientMetrics struct {
	size                                   uint32
	borderWidth, scrollWidth, scrollHeight int32
	captionWidth, captionHeight            int32
	captionFont                            logFont
	smCaptionWidth, smCaptionHeight        int32
	smCaptionFont                          logFont
	menuWidth, menuHeight                  int32
	menuFont, statusFont, messageFont      logFont
	paddedBorderWidth                      int32
}

// systemFonts returns the fonts of message boxes, menus and status bars.
func systemFonts() (message, menu, status logFont) {
	var ncm nonClientMetrics
	ncm.size = uint32(unsafe.Sizeof(ncm))
	procSystemParametersInfoW.Call(spiNonClientMetric, uintptr(ncm.size), uintptr(unsafe.Pointer(&ncm)), 0)
	return ncm.messageFont, ncm.menuFont, ncm.statusFont
}

func createFont(lf *logFont) uintptr {
	h, _, _ := procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(lf)))
	return h
}

func deleteObject(h uintptr) {
	if h != 0 {
		procDeleteObject.Call(h)
	}
}

func textWidth(hdc uintptr, s string) int32 {
	u := syscall.StringToUTF16(s)
	var sz size
	procGetTextExtentPoint32W.Call(hdc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&sz)))
	return sz.cx
}

func windowText(h win.HWND) string {
	n, _, _ := procGetWindowTextLengthW.Call(uintptr(h))
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func setWindowText(h win.HWND, s string) {
	procSetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(utf16Ptr(s))))
}
