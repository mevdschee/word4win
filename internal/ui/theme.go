//go:build windows

package ui

// Light and dark mode, as in the other 4win apps. The window follows the
// "app mode" of Windows (Settings > Personalization > Colors), also when it
// changes while the app runs. The toolbars, ruler and status bar are painted
// here in either mode; the page stays white, like paper (and like WordPad
// and Word in dark mode).
//
// WORD4WIN_THEME=dark (or light) overrides the system setting, which helps
// to try the dark look under Wine.

import (
	"os"
	"syscall"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// palette holds the colours of one mode (COLORREF, 0x00BBGGRR).
type palette struct {
	bar         uint32 // toolbar, ruler and status bar background
	barLine     uint32 // separators and borders
	text        uint32
	textDim     uint32 // group titles, disabled items
	hot         uint32 // button under the mouse
	pressed     uint32
	checked     uint32 // a button that is on
	checkedLine uint32
	accent      uint32 // the File button and links
	accentText  uint32
	dialog      uint32
	field       uint32
	page        uint32 // the ruler's text area
	workspace   uint32 // around the pages in the print preview
}

var (
	lightPalette = palette{
		bar: 0x00F9F9F9, barLine: 0x00E0E0E0, text: 0x001B1B1B, textDim: 0x00707070,
		hot: 0x00EAEAEA, pressed: 0x00DCDCDC, checked: 0x00F7E4CC, checkedLine: 0x00EFC999,
		accent: 0x00C06700, accentText: 0x00FFFFFF, dialog: 0x00F0F0F0, field: 0x00FFFFFF,
		page: 0x00FFFFFF, workspace: 0x00E6E6E6,
	}
	darkPalette = palette{
		bar: 0x00202020, barLine: 0x003A3A3A, text: 0x00FFFFFF, textDim: 0x00A0A0A0,
		hot: 0x002D2D2D, pressed: 0x00383838, checked: 0x005C4A3A, checkedLine: 0x00806040,
		accent: 0x00D47800, accentText: 0x00FFFFFF, dialog: 0x00202020, field: 0x002D2D2D,
		page: 0x00383838, workspace: 0x00141414,
	}
	darkMode bool
	colors   palette
)

// Undocumented uxtheme exports, by ordinal (0 when missing).
var (
	uxAllowDarkModeForWindow uintptr // 133
	uxSetPreferredAppMode    uintptr // 135 (AllowDarkModeForApp on 1809)
	uxFlushMenuThemes        uintptr // 136
	uxRefreshImmersivePolicy uintptr // 104
)

func initTheme() {
	if !isWine() {
		if _, _, build := windows.RtlGetNtVersionNumbers(); build&0xffff >= 17763 {
			if h, err := windows.LoadLibraryEx("uxtheme.dll", 0, windows.LOAD_LIBRARY_SEARCH_SYSTEM32); err == nil {
				get := func(ord uintptr) uintptr {
					p, _ := windows.GetProcAddressByOrdinal(h, ord)
					return p
				}
				uxAllowDarkModeForWindow = get(133)
				uxSetPreferredAppMode = get(135)
				uxFlushMenuThemes = get(136)
				uxRefreshImmersivePolicy = get(104)
				if uxSetPreferredAppMode != 0 {
					syscall.SyscallN(uxSetPreferredAppMode, 1) // AllowDark: follow the system
				}
			}
		}
	}
	darkMode = !systemDark()
	loadTheme()
}

func systemDark() bool {
	switch os.Getenv("WORD4WIN_THEME") {
	case "dark":
		return true
	case "light":
		return false
	}
	if highContrast() {
		return false
	}
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

func highContrast() bool {
	type highContrast struct {
		size, flags uint32
		scheme      uintptr
	}
	hc := highContrast{size: uint32(unsafe.Sizeof(highContrast{}))}
	r, _, _ := procSystemParametersInfoW.Call(spiGetHighContrast, uintptr(hc.size), uintptr(unsafe.Pointer(&hc)), 0)
	return r != 0 && hc.flags&1 != 0
}

// loadTheme reads the system mode into darkMode and colors; it reports
// whether the mode changed.
func loadTheme() bool {
	if uxRefreshImmersivePolicy != 0 {
		syscall.SyscallN(uxRefreshImmersivePolicy)
	}
	dark := systemDark()
	changed := dark != darkMode
	darkMode = dark
	if dark {
		colors = darkPalette
	} else {
		colors = lightPalette
		if highContrast() {
			colors.bar = sysColor(co.COLOR_BTNFACE)
			colors.text = sysColor(co.COLOR_BTNTEXT)
			colors.textDim = sysColor(co.COLOR_GRAYTEXT)
			colors.hot = sysColor(co.COLOR_HIGHLIGHT)
			colors.dialog = sysColor(co.COLOR_BTNFACE)
			colors.field = sysColor(co.COLOR_WINDOW)
		}
	}
	if changed && uxFlushMenuThemes != 0 {
		syscall.SyscallN(uxFlushMenuThemes)
	}
	return changed
}

func setTheme(h win.HWND, name string) {
	var p *uint16
	if name != "" {
		p = utf16Ptr(name)
	}
	procSetWindowTheme.Call(uintptr(h), uintptr(unsafe.Pointer(p)), 0)
}

func allowDark(h win.HWND) {
	if uxAllowDarkModeForWindow != 0 {
		syscall.SyscallN(uxAllowDarkModeForWindow, uintptr(h), boolArg(darkMode))
	}
}

// themeFrame makes a window's title bar dark or light, with the rounded
// Windows 11 corners.
func themeFrame(h win.HWND) {
	v := uint32(boolArg(darkMode))
	allowDark(h)
	if r, _, _ := procDwmSetWindowAttr.Call(uintptr(h), 20, uintptr(unsafe.Pointer(&v)), 4); r != 0 { // DWMWA_USE_IMMERSIVE_DARK_MODE
		procDwmSetWindowAttr.Call(uintptr(h), 19, uintptr(unsafe.Pointer(&v)), 4)
	}
	round := uint32(2) // DWMWCP_ROUND
	procDwmSetWindowAttr.Call(uintptr(h), 33, uintptr(unsafe.Pointer(&round)), 4)
}

// themeControl gives a control the look of the current mode.
func themeControl(h win.HWND) {
	cls, _ := h.GetClassName()
	name := ""
	switch cls {
	case "Button", "ScrollBar", "msctls_trackbar32", "msctls_updown32":
		name = "DarkMode_Explorer"
	case "Edit", "ComboBox", "ComboBoxEx32", "ListBox":
		name = "DarkMode_CFD"
	case "RICHEDIT50W":
		name = "DarkMode_Explorer"
	default:
		return
	}
	if !darkMode {
		name = "Explorer"
		if cls == "Edit" || cls == "ComboBox" {
			name = "CFD"
		}
	}
	allowDark(h)
	setTheme(h, name)
	h.SendMessage(co.WM_THEMECHANGED, 0, 0)
}

// themeDialog themes a window and its controls.
func themeDialog(h win.HWND) {
	themeFrame(h)
	if darkMode || uxAllowDarkModeForWindow != 0 {
		for _, c := range h.EnumChildWindows() {
			themeControl(c)
		}
	}
	procRedrawWindow.Call(uintptr(h), 0, 0, 0x0001|0x0004|0x0080|0x0400)
}

// newModal is ui.NewModal in the current mode.
func newModal(parent ui.Parent, opts *ui.VarOptsModal) *ui.Modal {
	pal := colors
	opts.ClassBrush(win.HBRUSH(solidBrush(pal.dialog)))
	wnd := ui.NewModal(parent, opts)
	on := wnd.On()
	on.WmCtlColorStatic(func(p ui.WmCtlColor) win.HBRUSH {
		procSetTextColor.Call(uintptr(p.Hdc()), uintptr(pal.text))
		procSetBkColor.Call(uintptr(p.Hdc()), uintptr(pal.dialog))
		return win.HBRUSH(solidBrush(pal.dialog))
	})
	if darkMode {
		on.WmCtlColorEdit(func(p ui.WmCtlColor) win.HBRUSH {
			procSetTextColor.Call(uintptr(p.Hdc()), uintptr(pal.text))
			procSetBkColor.Call(uintptr(p.Hdc()), uintptr(pal.field))
			return win.HBRUSH(solidBrush(pal.field))
		})
		on.WmCtlColorListBox(func(p ui.WmCtlColor) win.HBRUSH {
			procSetTextColor.Call(uintptr(p.Hdc()), uintptr(pal.text))
			procSetBkColor.Call(uintptr(p.Hdc()), uintptr(pal.field))
			return win.HBRUSH(solidBrush(pal.field))
		})
		on.WmCtlColorBtn(func(p ui.WmCtlColor) win.HBRUSH {
			procSetBkColor.Call(uintptr(p.Hdc()), uintptr(pal.dialog))
			return win.HBRUSH(solidBrush(pal.dialog))
		})
	}
	on.Wm(co.WM_SHOWWINDOW, func(p ui.Wm) uintptr {
		if p.WParam != 0 {
			themeDialog(wnd.Hwnd())
		}
		return wnd.Hwnd().DefWindowProc(p.Msg, p.WParam, p.LParam)
	})
	return wnd
}

func closeModal(wnd *ui.Modal) { wnd.Hwnd().SendMessage(co.WM_CLOSE, 0, 0) }

// mix blends two colours, t from 0 (a) to 1 (b).
func mix(a, b uint32, t float64) uint32 {
	ch := func(shift uint) uint32 {
		x := float64((a >> shift) & 0xFF)
		y := float64((b >> shift) & 0xFF)
		return uint32(x+(y-x)*t+0.5) & 0xFF
	}
	return ch(0) | ch(8)<<8 | ch(16)<<16
}
