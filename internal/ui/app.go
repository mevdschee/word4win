//go:build windows

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unsafe"

	"github.com/mevdschee/word4win/internal/config"
	"github.com/mevdschee/word4win/internal/doc"
	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"
	"golang.org/x/sys/windows"
)

const (
	appName  = "word4win"
	repoURL  = "https://github.com/mevdschee/word4win"
	author   = "Maurits van der Schee"
	untitled = "Document"
	iconId   = 1 // RT_GROUP_ICON "#1" in winres/winres.json

	timerWords = 1
)

// Commands of the toolbars, the menus and the keyboard.
const (
	cmdNew uint16 = 0x9000 + iota
	cmdNewWindow
	cmdOpen
	cmdSave
	cmdSaveAs
	cmdSaveRTF
	cmdSaveMarkdown
	cmdSaveText
	cmdSaveHTML
	cmdExportPDF
	cmdPrint
	cmdQuickPrint
	cmdPreview
	cmdPageSetup
	cmdExit
	cmdUndo
	cmdRedo
	cmdCut
	cmdCopy
	cmdPaste
	cmdPasteText
	cmdSelectAll
	cmdFind
	cmdFindNext
	cmdReplace
	cmdBold
	cmdItalic
	cmdUnderline
	cmdStrike
	cmdSubscript
	cmdSuperscript
	cmdGrow
	cmdShrink
	cmdTextColor
	cmdHighlight
	cmdFontDialog
	cmdAlignLeft
	cmdAlignCenter
	cmdAlignRight
	cmdJustify
	cmdIndent
	cmdOutdent
	cmdBullets
	cmdNumbering
	cmdLineSpacing
	cmdParagraph
	cmdPicture
	cmdTable
	cmdDateTime
	cmdLink
	cmdRule
	cmdRuler
	cmdStatusBar
	cmdHeading0 // Normal; cmdHeading0+n is heading n
	cmdHeading1
	cmdHeading2
	cmdHeading3
	cmdHeading4
	cmdHeading5
	cmdHeading6
	cmdQuote
	cmdCode
	cmdHelp
	cmdUpdate
	cmdAbout
	cmdMenu0 // cmdMenu0+i opens menu i of the menu row
	cmdMenu1
	cmdMenu2
	cmdMenu3
	cmdMenu4
	cmdMenu5
	cmdRecent // cmdRecent+i opens recent file i
	cmdLast   = cmdRecent + config.MaxRecent
)

// Menu item IDs for the dropdown menus, which return their choice.
const (
	menuBase       = 0xA000
	menuColor      = menuBase + 0x100 // + palette index; 0xFF is automatic
	menuColorMore  = menuBase + 0x1FF
	menuList       = menuBase + 0x200
	menuSpacing    = menuBase + 0x300
	menuSpaceAfter = menuBase + 0x3FF
	menuTable      = menuBase + 0x400
	menuWrap       = menuBase + 0x500
	menuUnits      = menuBase + 0x600
)

// App is the main window with its document.
type App struct {
	wnd     *ui.Main
	tb      *Toolbar
	ruler   *Ruler
	re      *RichEdit
	sb      *StatusBar
	cfg     *config.Settings
	cfgDir  string
	version string

	path   string     // empty for a new document
	format doc.Format // the format of the file on disk
	title  string
	warned bool // the user agreed to lose formatting in this format

	textColor uint32 // the colours of the split buttons
	highlight uint32
	updating  bool // the toolbars is being set from the selection
	words     string

	find  findState
	print printState
}

// Options are the command line arguments.
type Options struct {
	Path  string
	Print bool
}

// Run shows the main window and returns the exit code.
func Run(version string, opts Options) int {
	a := &App{version: version, textColor: 0x000000, highlight: 0x00FFFF}
	a.cfgDir, _ = config.Dir()
	a.cfg = config.Load(a.cfgDir)
	if a.cfg.Units == "" {
		a.cfg.Units = "in"
		if !localeIsUS() {
			a.cfg.Units = "cm"
		}
	}
	if a.cfg.PaperWidth <= 0 || a.cfg.PaperHeight <= 0 {
		a.cfg.PaperWidth, a.cfg.PaperHeight = 595.28, 841.89 // A4
		if localeIsUS() {
			a.cfg.PaperWidth, a.cfg.PaperHeight = 612, 792
		}
	}
	initTheme()
	if isWine() {
		doc.FieldLinks = false // Wine's RichEdit drops the text of link fields
	}

	a.wnd = ui.NewMain(ui.OptsMain().
		Title(untitled + " - " + appName).
		ClassIconId(iconId).
		ClassBrush(win.HBRUSH(0)).
		AccelTable(accelerators()).
		Size(ui.Dpi(1000, 700)).
		Center(true).
		CmdShow(a.showCommand()).
		Style(co.WS_OVERLAPPEDWINDOW | co.WS_CLIPCHILDREN))
	a.tb = newToolbar(a.wnd)
	a.ruler = newRuler(a.wnd)
	a.sb = newStatusBar(a.wnd)
	a.events(opts)
	return a.wnd.RunAsMain()
}

// localeIsUS tells whether Windows measures in inches (LOCALE_IMEASURE).
func localeIsUS() bool {
	var buf [4]uint16
	n, _, _ := procGetLocaleInfoW.Call(0x0400, 0x0D, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return n > 0 && buf[0] == '1'
}

func accelerators() win.HACCEL {
	const ctrl = co.ACCELF_VIRTKEY | co.ACCELF_CONTROL
	const ctrlShift = ctrl | co.ACCELF_SHIFT
	const ctrlAlt = ctrl | co.ACCELF_ALT
	const alt = co.ACCELF_VIRTKEY | co.ACCELF_ALT
	key := func(k byte) co.VK { return co.VK(k) }
	h, _ := win.CreateAcceleratorTable(
		win.ACCEL{FVirt: ctrl, Key: key('N'), Cmd: cmdNew},
		win.ACCEL{FVirt: ctrlShift, Key: key('N'), Cmd: cmdNewWindow},
		win.ACCEL{FVirt: ctrl, Key: key('O'), Cmd: cmdOpen},
		win.ACCEL{FVirt: ctrl, Key: key('S'), Cmd: cmdSave},
		win.ACCEL{FVirt: ctrlShift, Key: key('S'), Cmd: cmdSaveAs},
		win.ACCEL{FVirt: co.ACCELF_VIRTKEY, Key: co.VK_F12, Cmd: cmdSaveAs},
		win.ACCEL{FVirt: ctrl, Key: key('P'), Cmd: cmdPrint},
		win.ACCEL{FVirt: ctrl, Key: co.VK_F2, Cmd: cmdPreview},
		win.ACCEL{FVirt: ctrl, Key: key('W'), Cmd: cmdExit},
		win.ACCEL{FVirt: ctrl, Key: key('F'), Cmd: cmdFind},
		win.ACCEL{FVirt: co.ACCELF_VIRTKEY, Key: co.VK_F3, Cmd: cmdFindNext},
		win.ACCEL{FVirt: ctrl, Key: key('H'), Cmd: cmdReplace},
		win.ACCEL{FVirt: ctrl, Key: key('B'), Cmd: cmdBold},
		win.ACCEL{FVirt: ctrl, Key: key('I'), Cmd: cmdItalic},
		win.ACCEL{FVirt: ctrl, Key: key('U'), Cmd: cmdUnderline},
		win.ACCEL{FVirt: ctrl, Key: co.VK_OEM_PLUS, Cmd: cmdSubscript},
		win.ACCEL{FVirt: ctrlShift, Key: co.VK_OEM_PLUS, Cmd: cmdSuperscript},
		win.ACCEL{FVirt: ctrlShift, Key: co.VK_OEM_PERIOD, Cmd: cmdGrow},
		win.ACCEL{FVirt: ctrlShift, Key: co.VK_OEM_COMMA, Cmd: cmdShrink},
		win.ACCEL{FVirt: ctrl, Key: key('D'), Cmd: cmdFontDialog},
		win.ACCEL{FVirt: ctrl, Key: key('L'), Cmd: cmdAlignLeft},
		win.ACCEL{FVirt: ctrl, Key: key('E'), Cmd: cmdAlignCenter},
		win.ACCEL{FVirt: ctrl, Key: key('R'), Cmd: cmdAlignRight},
		win.ACCEL{FVirt: ctrl, Key: key('J'), Cmd: cmdJustify},
		win.ACCEL{FVirt: ctrlShift, Key: key('L'), Cmd: cmdBullets},
		win.ACCEL{FVirt: ctrl, Key: key('K'), Cmd: cmdLink},
		win.ACCEL{FVirt: ctrlAlt, Key: key('0'), Cmd: cmdHeading0},
		win.ACCEL{FVirt: ctrlAlt, Key: key('1'), Cmd: cmdHeading1},
		win.ACCEL{FVirt: ctrlAlt, Key: key('2'), Cmd: cmdHeading2},
		win.ACCEL{FVirt: ctrlAlt, Key: key('3'), Cmd: cmdHeading3},
		win.ACCEL{FVirt: ctrlAlt, Key: key('4'), Cmd: cmdHeading4},
		win.ACCEL{FVirt: ctrlAlt, Key: key('5'), Cmd: cmdHeading5},
		win.ACCEL{FVirt: ctrlAlt, Key: key('6'), Cmd: cmdHeading6},
		win.ACCEL{FVirt: ctrlShift, Key: key('V'), Cmd: cmdPasteText},
		win.ACCEL{FVirt: co.ACCELF_VIRTKEY, Key: co.VK_F1, Cmd: cmdHelp},
		win.ACCEL{FVirt: co.ACCELF_VIRTKEY, Key: co.VK_F10, Cmd: cmdMenu0},
		win.ACCEL{FVirt: alt, Key: key('F'), Cmd: cmdMenu0},
		win.ACCEL{FVirt: alt, Key: key('E'), Cmd: cmdMenu1},
		win.ACCEL{FVirt: alt, Key: key('V'), Cmd: cmdMenu2},
		win.ACCEL{FVirt: alt, Key: key('I'), Cmd: cmdMenu3},
		win.ACCEL{FVirt: alt, Key: key('O'), Cmd: cmdMenu4},
		win.ACCEL{FVirt: alt, Key: key('H'), Cmd: cmdMenu5},
	)
	return h
}

const wmStart = co.WM(wmUser + 100)

func (a *App) events(opts Options) {
	on := a.wnd.On()
	on.WmCreate(func(_ ui.WmCreate) int {
		a.re = newRichEdit(a.wnd.Hwnd())
		a.subclassEditor()
		a.newDocument()
		a.restoreWindow()
		procDragAcceptFiles.Call(uintptr(a.wnd.Hwnd()), 1)
		a.applyTheme()
		a.ruler.OnIndent = a.setIndents
		a.tb.OnCommand = a.command
		a.tb.OnDrop = a.dropdown
		a.tb.OnMenu = a.menuBar
		a.tb.OnCombo = a.comboChanged
		a.layout()
		a.wnd.Hwnd().PostMessage(wmStart, 0, 0)
		return 0
	})
	on.Wm(wmStart, func(_ ui.Wm) uintptr {
		a.re.Hwnd().SetFocus()
		a.updateFormat()
		a.updateTitle()
		a.start(opts)
		return 0
	})
	on.WmSize(func(p ui.WmSize) {
		if p.Request() != co.SIZE_REQ_MINIMIZED {
			a.layout()
		}
	})
	on.WmEraseBkgnd(func(_ ui.WmEraseBkgnd) int { return 1 })
	on.WmSetFocus(func(_ ui.WmSetFocus) {
		if a.re != nil {
			a.re.Hwnd().SetFocus()
		}
	})
	on.WmClose(func() {
		if !a.confirmSave() {
			return
		}
		a.saveSettings()
		a.closeFind()
		a.wnd.Hwnd().DestroyWindow()
	})
	on.Wm(co.WM_QUERYENDSESSION, func(_ ui.Wm) uintptr {
		if a.confirmSave() {
			a.saveSettings()
			return 1
		}
		return 0
	})
	on.WmDropFiles(func(p ui.WmDropFiles) {
		files, err := p.HDrop().DragQueryFile()
		p.HDrop().DragFinish()
		if err != nil || len(files) == 0 {
			return
		}
		procSetForegroundWindow.Call(uintptr(a.wnd.Hwnd()))
		if a.confirmSave() {
			a.open(files[0])
		}
	})
	on.WmGetMinMaxInfo(func(p ui.WmGetMinMaxInfo) {
		p.Info().PtMinTrackSize = win.POINT{X: int32(ui.DpiX(420)), Y: int32(ui.DpiY(300))}
	})
	on.WmTimer(timerWords, func() {
		a.wnd.Hwnd().KillTimer(timerWords)
		a.countWords()
	})
	for _, msg := range []co.WM{wmSettingChange, co.WM_SYSCOLORCHANGE, co.WM_THEMECHANGED} {
		on.Wm(msg, func(p ui.Wm) uintptr {
			if loadTheme() || p.Msg != wmSettingChange {
				a.applyTheme()
			}
			return a.wnd.Hwnd().DefWindowProc(p.Msg, p.WParam, p.LParam)
		})
	}
	// The editor's notifications.
	on.WmCommand(richEditID, co.CMD(enChange), func() {
		a.updateTitle()
		a.wnd.Hwnd().SetTimer(timerWords, 400)
	})
	on.WmNotify(richEditID, co.NM(enSelChange), func(_ unsafe.Pointer) uintptr {
		a.updateFormat()
		return 0
	})
	on.WmCommand(richEditID, co.CMD(enHScroll), func() { a.updateRuler() })
	on.WmNotify(richEditID, co.NM(enLink), func(p unsafe.Pointer) uintptr {
		return a.link(p)
	})
	on.WmContextMenu(func(p ui.WmContextMenu) {
		if p.RightClickedWindow() == a.re.Hwnd() {
			a.contextMenu(p.CursorPos())
		}
	})
	// Enter and Esc in the combo boxes of the toolbars come here through
	// IsDialogMessage.
	on.WmCommandAccelMenu(uint16(co.ID_OK), func() { a.comboEnter() })
	on.WmCommandAccelMenu(uint16(co.ID_CANCEL), func() { a.re.Hwnd().SetFocus() })
	on.Wm(findMsg(), func(p ui.Wm) uintptr {
		a.findMessage(p.LParam)
		return 0
	})

	for id := cmdNew; id < cmdLast; id++ {
		on.WmCommandAccelMenu(id, func() { a.command(id) })
	}
}

// command runs a command of the toolbars, a menu or the keyboard.
func (a *App) command(id uint16) {
	switch {
	case id >= cmdRecent && id < cmdRecent+config.MaxRecent:
		i := int(id - cmdRecent)
		if i < len(a.cfg.Recent) && a.confirmSave() {
			a.open(a.cfg.Recent[i])
		}
		return
	case id >= cmdMenu0 && id <= cmdMenu5:
		a.tb.ShowMenu(int(id - cmdMenu0))
		return
	case id >= cmdHeading0 && id <= cmdHeading6:
		a.applyStyle(int(id - cmdHeading0))
		return
	}
	switch id {
	case cmdNew:
		if a.confirmSave() {
			a.newDocument()
		}
	case cmdNewWindow:
		a.newWindow()
	case cmdOpen:
		a.openDialog()
	case cmdSave:
		a.save()
	case cmdSaveAs:
		a.saveAs("")
	case cmdSaveRTF:
		a.saveAs(doc.FormatRTF)
	case cmdSaveMarkdown:
		a.saveAs(doc.FormatMarkdown)
	case cmdSaveText:
		a.saveAs(doc.FormatText)
	case cmdSaveHTML:
		a.saveAs(doc.FormatHTML)
	case cmdExportPDF:
		a.exportPDF()
	case cmdPrint:
		a.printDialog()
	case cmdQuickPrint:
		a.printNow()
	case cmdPreview:
		a.preview()
	case cmdPageSetup:
		a.pageSetup()
	case cmdExit:
		a.wnd.Hwnd().PostMessage(co.WM_CLOSE, 0, 0)
	case cmdUndo:
		a.re.Undo()
	case cmdRedo:
		a.re.Redo()
	case cmdCut:
		a.re.Cut()
	case cmdCopy:
		a.re.Copy()
	case cmdPaste:
		a.re.Paste()
	case cmdPasteText:
		a.re.PasteText()
	case cmdSelectAll:
		a.re.SelectAll()
	case cmdFind:
		a.showFind(false)
	case cmdFindNext:
		a.findNext()
	case cmdReplace:
		a.showFind(true)
	case cmdBold:
		a.toggleEffect(cfmBold, cfeBold)
	case cmdItalic:
		a.toggleEffect(cfmItalic, cfeItalic)
	case cmdUnderline:
		a.toggleEffect(cfmUnderline, cfeUnderline)
	case cmdStrike:
		a.toggleEffect(cfmStrikeout, cfeStrikeout)
	case cmdSubscript:
		a.toggleEffect(cfmSuperscript, cfeSubscript)
	case cmdSuperscript:
		a.toggleEffect(cfmSuperscript, cfeSuperscript)
	case cmdGrow:
		a.growFont(1)
	case cmdShrink:
		a.growFont(-1)
	case cmdTextColor:
		a.setTextColor(a.textColor, false)
	case cmdHighlight:
		a.setHighlight(a.highlight, false)
	case cmdFontDialog:
		a.fontDialog()
	case cmdAlignLeft:
		a.setAlign(pfaLeft)
	case cmdAlignCenter:
		a.setAlign(pfaCenter)
	case cmdAlignRight:
		a.setAlign(pfaRight)
	case cmdJustify:
		a.setAlign(pfaJustify)
	case cmdIndent:
		a.indent(720)
	case cmdOutdent:
		a.indent(-720)
	case cmdBullets:
		a.toggleList(pfnBullet)
	case cmdNumbering:
		a.toggleList(pfnArabic)
	case cmdParagraph:
		a.paragraphDialog()
	case cmdPicture:
		a.insertPicture()
	case cmdTable:
		a.insertTable(3, 3)
	case cmdDateTime:
		a.dateTimeDialog()
	case cmdLink:
		a.linkDialog()
	case cmdRule:
		a.insertRule()
	case cmdQuote:
		a.applyStyle(7)
	case cmdCode:
		a.applyStyle(8)
	case cmdRuler:
		a.cfg.Ruler = !a.cfg.Ruler
		a.layout()
		a.updateFormat()
	case cmdStatusBar:
		a.cfg.StatusBar = !a.cfg.StatusBar
		a.layout()
		a.updateFormat()
	case cmdHelp:
		shellOpen(a.wnd.Hwnd(), repoURL+"#readme")
	case cmdUpdate:
		a.checkForUpdates()
	case cmdAbout:
		showAbout(a)
	}
	if id != cmdExit && a.re != nil && a.wnd.Hwnd().IsWindow() {
		if focus, _, _ := procGetFocus.Call(); focus == 0 || win.HWND(focus) == a.tb.Hwnd() {
			a.re.Hwnd().SetFocus()
		}
	}
}

// ---- window ----

func (a *App) layout() {
	if a.re == nil {
		return
	}
	rc, _ := a.wnd.Hwnd().GetClientRect()
	flags := co.SWP_NOZORDER | co.SWP_NOACTIVATE
	y := int32(0)
	tbH := a.tb.Height()
	a.tb.Hwnd().SetWindowPos(0, win.POINT{}, win.SIZE{Cx: rc.Right, Cy: tbH}, flags)
	y += tbH
	if a.cfg.Ruler {
		a.ruler.Hwnd().SetWindowPos(0, win.POINT{Y: y}, win.SIZE{Cx: rc.Right, Cy: a.ruler.Height()}, flags|co.SWP_SHOWWINDOW)
		y += a.ruler.Height()
	} else {
		a.ruler.Hwnd().ShowWindow(co.SW_HIDE)
	}
	bottom := rc.Bottom
	if a.cfg.StatusBar {
		h := a.sb.Height()
		a.sb.Hwnd().SetWindowPos(0, win.POINT{Y: rc.Bottom - h}, win.SIZE{Cx: rc.Right, Cy: h}, flags|co.SWP_SHOWWINDOW)
		bottom -= h
	} else {
		a.sb.Hwnd().ShowWindow(co.SW_HIDE)
	}
	a.re.Hwnd().SetWindowPos(0, win.POINT{Y: y}, win.SIZE{Cx: rc.Right, Cy: max(0, bottom-y)}, flags)
	a.applyWrap()
	a.updateRuler()
}

// editorMargin is the space left of the text in the editor, in pixels.
func editorMargin() int { return int(px(14)) }

// textWidthTwips is the width of the text on the page.
func (a *App) textWidthTwips() int {
	return int((a.cfg.PaperWidth - a.cfg.MarginLeft - a.cfg.MarginRight) * 20)
}

func (a *App) applyWrap() {
	a.re.SetMargins(editorMargin(), editorMargin())
	a.re.SetWrap(a.cfg.Wrap, a.textWidthTwips())
	// RichEdit keeps its horizontal scroll bar enabled when the text fits:
	// hide it when the lines are not wider than the window.
	show := true
	switch a.cfg.Wrap {
	case config.WrapWindow:
		show = false
	case config.WrapRuler:
		rc, _ := a.re.Hwnd().GetClientRect()
		show = mulDiv(a.textWidthTwips(), screenDpi(), 1440)+2*editorMargin() > int(rc.Right)
	}
	a.re.ShowHScroll(show)
}

// updateRuler gives the ruler the text area and the current paragraph.
func (a *App) updateRuler() {
	if a.re == nil || !a.cfg.Ruler {
		return
	}
	r := a.ruler
	r.scale = float64(screenDpi()) / 1440
	si := struct {
		size, mask    uint32
		min, max      int32
		page          uint32
		pos, trackPos int32
	}{mask: 0x0004} // SIF_POS
	si.size = uint32(unsafe.Sizeof(si))
	procGetScrollInfo.Call(uintptr(a.re.Hwnd()), 0, uintptr(unsafe.Pointer(&si))) // SB_HORZ
	r.origin = int32(editorMargin()) - si.pos
	if a.cfg.Wrap == config.WrapRuler {
		r.width = a.textWidthTwips()
	} else {
		rc, _ := a.re.Hwnd().GetClientRect()
		r.width = int(float64(rc.Right-int32(2*editorMargin())) / r.scale)
	}
	pf := a.re.ParaFormat()
	r.first = int(pf.StartIndent)
	r.left = int(pf.StartIndent + pf.Offset)
	r.right = int(pf.RightIndent)
	r.cm = a.cfg.Units == "cm"
	invalidate(r.Hwnd())
}

func (a *App) setIndents(first, left, right int) {
	pf := newParaFormat()
	pf.Mask = pfmStartIndent | pfmOffset | pfmRightIndent
	pf.StartIndent = int32(first)
	pf.Offset = int32(left - first)
	pf.RightIndent = int32(right)
	a.re.SetParaFormat(&pf)
	a.re.Hwnd().SetFocus()
	a.updateRuler()
}

func (a *App) restoreWindow() {
	h := a.wnd.Hwnd()
	wp, err := h.GetWindowPlacement()
	if err == nil {
		rc := wp.RcNormalPosition
		if a.cfg.Width >= 300 && a.cfg.Height >= 200 {
			rc = win.RECT{Left: int32(a.cfg.X), Top: int32(a.cfg.Y), Right: int32(a.cfg.X + a.cfg.Width), Bottom: int32(a.cfg.Y + a.cfg.Height)}
		}
		if pos := strings.Split(os.Getenv("WORD4WIN_POS"), ","); len(pos) == 2 {
			x, errX := strconv.Atoi(pos[0])
			y, errY := strconv.Atoi(pos[1])
			if errX == nil && errY == nil {
				rc.Right, rc.Bottom = int32(x)+rc.Right-rc.Left, int32(y)+rc.Bottom-rc.Top
				rc.Left, rc.Top = int32(x), int32(y)
			}
		}
		wp.RcNormalPosition = rc
		wp.ShowCmd = co.SW_HIDE
		wp.SetLength()
		h.SetWindowPlacement(&wp)
	}
}

func (a *App) showCommand() co.SW {
	show := co.SW_SHOWNORMAL
	if a.cfg.Maximized {
		show = co.SW_SHOWMAXIMIZED
	}
	var si syscall.StartupInfo
	if syscall.GetStartupInfo(&si) == nil && si.Flags&1 != 0 {
		switch co.SW(si.ShowWindow) {
		case co.SW_SHOWMINIMIZED, co.SW_SHOWMAXIMIZED, co.SW_MINIMIZE, co.SW_SHOWMINNOACTIVE:
			show = co.SW(si.ShowWindow)
		}
	}
	return show
}

func (a *App) saveSettings() {
	h := a.wnd.Hwnd()
	if wp, err := h.GetWindowPlacement(); err == nil {
		rc := wp.RcNormalPosition
		a.cfg.X, a.cfg.Y = int(rc.Left), int(rc.Top)
		a.cfg.Width, a.cfg.Height = int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)
		a.cfg.Maximized = h.IsZoomed()
	}
	a.cfg.Save(a.cfgDir)
}

func (a *App) applyTheme() {
	themeFrame(a.wnd.Hwnd())
	for _, id := range []uint16{comboFont, comboSize, comboStyle} {
		if c := a.tb.Combo(id); c != 0 {
			themeControl(c)
		}
	}
	allowDark(a.re.Hwnd())
	if darkMode {
		setTheme(a.re.Hwnd(), "DarkMode_Explorer") // dark scrollbars; the page stays white
	} else {
		setTheme(a.re.Hwnd(), "Explorer")
	}
	if a.find.dlg != 0 {
		themeDialog(a.find.dlg)
	}
	procRedrawWindow.Call(uintptr(a.wnd.Hwnd()), 0, 0, 0x0001|0x0004|0x0080|0x0400)
}

// name is the file name for the title bar and the messages.
func (a *App) name() string {
	if a.path == "" {
		return untitled
	}
	return filepath.Base(a.path)
}

func (a *App) updateTitle() {
	title := a.name() + " - " + appName
	if a.re.Modified() {
		title = "*" + title
	}
	if title != a.title {
		a.title = title
		a.wnd.Hwnd().SetWindowText(title)
	}
	a.tb.SetEnabled(cmdUndo, a.re.CanUndo())
	a.tb.SetEnabled(cmdRedo, a.re.CanRedo())
}

func (a *App) countWords() {
	text := a.re.Text()
	words, chars := 0, 0
	in := false
	for _, r := range text {
		if r == '\r' || r == '\n' {
			in = false
			continue
		}
		chars++
		if unicode.IsSpace(r) {
			in = false
		} else if !in {
			in = true
			words++
		}
	}
	a.sb.SetText(fmt.Sprintf("%s words, %s characters", thousands(words), thousands(chars)))
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// ---- the toolbars follows the selection ----

func (a *App) updateFormat() {
	if a.re == nil {
		return
	}
	a.updating = true
	defer func() { a.updating = false }()
	cf := a.re.CharFormat()
	pf := a.re.ParaFormat()
	on := func(mask, effect uint32) bool { return cf.Mask&mask != 0 && cf.Effects&effect != 0 }
	a.tb.SetChecked(cmdBold, on(cfmBold, cfeBold))
	a.tb.SetChecked(cmdItalic, on(cfmItalic, cfeItalic))
	a.tb.SetChecked(cmdUnderline, on(cfmUnderline, cfeUnderline))
	a.tb.SetChecked(cmdStrike, on(cfmStrikeout, cfeStrikeout))
	a.tb.SetChecked(cmdSubscript, cf.Mask&cfmSuperscript == cfmSuperscript && cf.Effects&cfeSubscript != 0)
	a.tb.SetChecked(cmdSuperscript, cf.Mask&cfmSuperscript == cfmSuperscript && cf.Effects&cfeSuperscript != 0)
	align := pf.Alignment
	if pf.Mask&pfmAlignment == 0 {
		align = 0
	}
	a.tb.SetChecked(cmdAlignLeft, align == pfaLeft)
	a.tb.SetChecked(cmdAlignCenter, align == pfaCenter)
	a.tb.SetChecked(cmdAlignRight, align == pfaRight)
	a.tb.SetChecked(cmdJustify, align == pfaJustify)
	a.tb.SetChecked(cmdBullets, pf.Mask&pfmNumbering != 0 && pf.Numbering == pfnBullet)
	a.tb.SetChecked(cmdNumbering, pf.Mask&pfmNumbering != 0 && pf.Numbering > pfnBullet)
	a.tb.SetChecked(cmdRuler, a.cfg.Ruler)
	a.tb.SetChecked(cmdStatusBar, a.cfg.StatusBar)
	a.tb.SetEnabled(cmdCut, a.re.HasSelection())
	a.tb.SetEnabled(cmdCopy, a.re.HasSelection())

	face := ""
	if cf.Mask&cfmFace != 0 {
		face = cf.face()
	}
	setComboText(a.tb.Combo(comboFont), face)
	size := ""
	if cf.Mask&cfmSize != 0 {
		size = strconv.FormatFloat(float64(cf.Height)/20, 'f', -1, 64)
	}
	setComboText(a.tb.Combo(comboSize), size)
	a.tb.Combo(comboStyle).SendMessage(co.CB_SETCURSEL, win.WPARAM(a.styleOfSelection(&cf, &pf)), 0)
	a.updateTitle()
	a.updateRuler()
}

func setComboText(h win.HWND, s string) {
	if h == 0 || windowText(h) == s {
		return
	}
	setWindowText(h, s)
}

// styleOfSelection recognises the paragraph styles of the style list, by
// their look.
func (a *App) styleOfSelection(cf *charFormat, pf *paraFormat) int {
	if cf.Mask&cfmFace != 0 && doc.IsMonospace(cf.face()) {
		return 8
	}
	if cf.Mask&cfmBold != 0 && cf.Effects&cfeBold != 0 && cf.Mask&cfmSize != 0 {
		for level := 1; level <= 6; level++ {
			if int(cf.Height) == int(doc.HeadingSize(level)*20) {
				return level
			}
		}
	}
	if pf.Mask&pfmNumbering != 0 && pf.Numbering == 0 && pf.StartIndent > 0 && cf.Mask&cfmColor != 0 && cf.TextColor == colorref(0x59, 0x59, 0x59) {
		return 7
	}
	return 0
}

// ---- formatting ----

func (a *App) toggleEffect(mask, effect uint32) {
	cf := a.re.CharFormat()
	set := !(cf.Mask&mask == mask && cf.Effects&effect != 0)
	n := newCharFormat()
	n.Mask = mask
	if set {
		n.Effects = effect
	}
	a.re.SetCharFormat(&n)
	a.updateFormat()
}

var fontSizes = []float64{8, 9, 10, 10.5, 11, 12, 14, 16, 18, 20, 22, 24, 26, 28, 36, 48, 72}

func (a *App) growFont(dir int) {
	cf := a.re.CharFormat()
	cur := float64(cf.Height) / 20
	if cf.Mask&cfmSize == 0 || cur <= 0 {
		cur = doc.DefaultSize
	}
	next := cur
	if dir > 0 {
		next = cur + 1
		for _, s := range fontSizes {
			if s > cur {
				next = s
				break
			}
		}
	} else {
		next = max(1, cur-1)
		for i := len(fontSizes) - 1; i >= 0; i-- {
			if fontSizes[i] < cur {
				next = fontSizes[i]
				break
			}
		}
	}
	a.setFontSize(next)
}

func (a *App) setFontSize(pt float64) {
	if pt < 1 || pt > 1638 {
		beep()
		return
	}
	n := newCharFormat()
	n.Mask = cfmSize
	n.Height = int32(pt * 20)
	a.re.SetCharFormat(&n)
	a.updateFormat()
}

func (a *App) setFontFace(face string) {
	if strings.TrimSpace(face) == "" {
		return
	}
	n := newCharFormat()
	n.setFace(strings.TrimSpace(face))
	n.Mask |= cfmCharset
	n.CharSet = 1 // DEFAULT_CHARSET
	a.re.SetCharFormat(&n)
	a.updateFormat()
}

func (a *App) setTextColor(c uint32, auto bool) {
	n := newCharFormat()
	n.Mask = cfmColor
	if auto {
		n.Effects = cfeAutoColor
	} else {
		n.TextColor = c
		a.textColor = c
		a.tb.SetSwatch(cmdTextColor, c)
	}
	a.re.SetCharFormat(&n)
}

func (a *App) setHighlight(c uint32, none bool) {
	n := newCharFormat()
	n.Mask = cfmBackColor
	if none {
		n.Effects = cfeAutoBackColor
	} else {
		n.BackColor = c
		a.highlight = c
		a.tb.SetSwatch(cmdHighlight, c)
	}
	a.re.SetCharFormat(&n)
}

func (a *App) setAlign(al uint16) {
	pf := newParaFormat()
	pf.Mask = pfmAlignment
	pf.Alignment = al
	a.re.SetParaFormat(&pf)
	a.updateFormat()
}

func (a *App) indent(delta int32) {
	cur := a.re.ParaFormat()
	pf := newParaFormat()
	pf.Mask = pfmStartIndent
	pf.StartIndent = max(0, cur.StartIndent+delta)
	a.re.SetParaFormat(&pf)
	a.updateFormat()
}

// setList makes the selected paragraphs list items of a kind, or normal
// paragraphs with 0.
func (a *App) setList(kind uint16) {
	pf := newParaFormat()
	pf.Mask = pfmNumbering | pfmStartIndent | pfmOffset | pfmNumStyle | pfmNumStart | pfmNumTab
	pf.Numbering = kind
	if kind != 0 {
		pf.StartIndent = 360
		pf.Offset = 360
		pf.NumberingStart = 1
		pf.NumberingStyle = pfnsPeriod
		pf.NumberingTab = 360
	}
	a.re.SetParaFormat(&pf)
	a.updateFormat()
}

func (a *App) toggleList(kind uint16) {
	cur := a.re.ParaFormat()
	if cur.Mask&pfmNumbering != 0 && (cur.Numbering == kind || kind != pfnBullet && cur.Numbering > pfnBullet) {
		a.setList(0)
		return
	}
	a.setList(kind)
}

func (a *App) setLineSpacing(lines float64) {
	pf := newParaFormat()
	pf.Mask = pfmLineSpacing
	if lines == 1 {
		pf.LineRule = 0
	} else {
		pf.LineRule = 5 // a multiple, in twentieths of a line
		pf.LineSpacing = int32(lines*20 + 0.5)
	}
	a.re.SetParaFormat(&pf)
}

// selectParagraphs widens the selection to whole paragraphs; it returns
// the old selection.
func (a *App) selectParagraphs() (int, int) {
	s, e := a.re.Selection()
	text := []rune(a.re.Text())
	ps := s
	for ps > 0 && ps <= len(text) && text[ps-1] != '\r' && text[ps-1] != '\n' {
		ps--
	}
	pe := e
	if pe > s && pe <= len(text) && pe > 0 && (text[pe-1] == '\r' || text[pe-1] == '\n') {
		pe--
	}
	for pe < len(text) && text[pe] != '\r' && text[pe] != '\n' {
		pe++
	}
	a.re.SetSelection(ps, pe)
	return s, e
}

// applyStyle gives the selected paragraphs a style of the style list:
// 0 Normal, 1 to 6 headings, 7 Quote, 8 Code. They are made of plain
// formatting, which the RTF reader recognises when saving as Markdown.
func (a *App) applyStyle(style int) {
	cur := a.re.CharFormat()
	curPf := a.re.ParaFormat()
	was := a.styleOfSelection(&cur, &curPf)
	s, e := a.selectParagraphs()
	defer func() {
		a.re.SetSelection(s, e)
		a.updateFormat()
	}()
	cf := newCharFormat()
	pf := newParaFormat()
	pf.Mask = pfmSpaceBefore | pfmSpaceAfter
	cf.Mask = cfmSize | cfmFace | cfmColor | cfmBold | cfmBackColor
	cf.Effects = cfeAutoColor | cfeAutoBackColor
	cf.setFace(doc.DefaultFont)
	cf.Height = int32(doc.DefaultSize * 20)
	pf.SpaceAfter = 120
	switch {
	case style >= 1 && style <= 6:
		cf.Effects |= cfeBold
		cf.Height = int32(doc.HeadingSize(style) * 20)
		pf.SpaceBefore, pf.SpaceAfter = 240, 60
		pf.Mask |= pfmNumbering | pfmStartIndent | pfmOffset
	case style == 7:
		cf.Mask &^= cfmBold
		cf.Effects = cfeAutoBackColor
		cf.TextColor = colorref(0x59, 0x59, 0x59)
		pf.Mask |= pfmStartIndent | pfmOffset | pfmNumbering
		pf.StartIndent = 360
	case style == 8:
		cf.Mask &^= cfmBold
		cf.setFace(doc.CodeFont)
		cf.Height = int32(doc.CodeSize * 20)
		cf.Effects = cfeAutoColor
		cf.BackColor = colorref(0xF0, 0xF0, 0xF0)
		pf.SpaceAfter = 0
	default:
		if was >= 1 && was <= 6 {
			cf.Effects &^= cfeBold // bold stays bold, but a heading is not
		} else {
			cf.Mask &^= cfmBold
		}
		if was == 7 {
			pf.Mask |= pfmStartIndent | pfmOffset
		}
		if was != 8 {
			cf.Mask &^= cfmBackColor // keep highlights
		}
	}
	a.re.SetCharFormat(&cf)
	a.re.SetParaFormat(&pf)
}

// ---- dropdown menus ----

var palette16 = []uint32{
	0x000000, 0x7F7F7F, 0x000080, 0x0000FF, 0x0080FF, 0x00FFFF, 0x00FF00, 0x00B050,
	0xFF8000, 0xC00000, 0xA03070, 0xFFFFFF, 0xC0C0C0, 0x7AC0FF, 0x99FFCC, 0xFFCC99,
}

var highlightPalette = []uint32{0x00FFFF, 0x00FF00, 0xFFFF00, 0xFF00FF, 0x0000FF, 0xFF0000, 0x800000, 0x008080, 0x008000, 0x800080, 0x000080, 0x008080, 0x808080, 0xC0C0C0, 0x000000}

// popup shows a menu at a screen point and returns the chosen ID.
func (a *App) popup(m win.HMENU, at win.POINT) uint16 {
	r, _ := m.TrackPopupMenu(co.TPM_RETURNCMD|co.TPM_LEFTALIGN|co.TPM_TOPALIGN, int(at.X), int(at.Y), a.wnd.Hwnd())
	return uint16(r)
}

func (a *App) colorMenu(highlight bool) win.HMENU {
	m, _ := win.CreatePopupMenu()
	pal := palette16
	first := "Automatic"
	if highlight {
		pal = highlightPalette
		first = "No color"
	}
	appendMenu(m, mfString, menuColor+0xFF, first)
	appendMenu(m, mfSeparator, 0, "")
	names := map[uint32]string{0x000000: "Black", 0x7F7F7F: "Gray", 0x000080: "Maroon", 0x0000FF: "Red", 0x0080FF: "Orange",
		0x00FFFF: "Yellow", 0x00FF00: "Bright green", 0x00B050: "Green", 0xFF8000: "Blue", 0xC00000: "Dark blue",
		0xA03070: "Purple", 0xFFFFFF: "White", 0xC0C0C0: "Silver", 0x7AC0FF: "Peach", 0x99FFCC: "Light green", 0xFFCC99: "Light blue",
		0xFFFF00: "Turquoise", 0xFF00FF: "Pink", 0xFF0000: "Blue", 0x800000: "Navy", 0x008080: "Olive", 0x008000: "Dark green",
		0x800080: "Violet", 0x808080: "Dark gray"}
	for i, c := range pal {
		name := names[c]
		if name == "" {
			name = fmt.Sprintf("#%02X%02X%02X", c&0xFF, c>>8&0xFF, c>>16&0xFF)
		}
		appendMenu(m, mfString, uintptr(menuColor+i), name)
		setMenuBitmap(m, uint16(menuColor+i), swatchBitmap(c, int(px(16)), int(px(16)), 0x808080))
	}
	if !highlight {
		appendMenu(m, mfSeparator, 0, "")
		appendMenu(m, mfString, menuColorMore, "More colors...")
	}
	return m
}

func setMenuBitmap(m win.HMENU, id uint16, bmp uintptr) {
	mii := struct {
		size, mask, fType, fState, id uint32
		subMenu, checked, unchecked   uintptr
		itemData                      uintptr
		typeData                      *uint16
		cch                           uint32
		bmpItem                       uintptr
	}{mask: 0x0080, bmpItem: bmp} // MIIM_BITMAP
	mii.size = uint32(unsafe.Sizeof(mii))
	procSetMenuItemInfoW.Call(uintptr(m), uintptr(id), 0, uintptr(unsafe.Pointer(&mii)))
}

// dropdown opens the menu of a toolbar button.
func (a *App) dropdown(id uint16, at win.POINT) {
	defer a.re.Hwnd().SetFocus()
	var m win.HMENU
	switch id {
	case cmdPaste:
		m, _ = win.CreatePopupMenu()
		appendMenu(m, mfString, uintptr(cmdPaste), "&Paste\tCtrl+V")
		appendMenu(m, mfString, uintptr(cmdPasteText), "Paste as &text\tCtrl+Shift+V")
	case cmdTextColor:
		m = a.colorMenu(false)
	case cmdHighlight:
		m = a.colorMenu(true)
	case cmdBullets, cmdNumbering:
		m = a.listMenu()
	case cmdLineSpacing:
		m = a.spacingMenu()
	case cmdTable:
		m = a.tableMenu()
	default:
		return
	}
	defer m.DestroyMenu()
	a.choose(a.popup(m, at), id == cmdHighlight)
}

// choose carries out a menu choice: a command, or an item of one of the
// submenus below.
func (a *App) choose(r uint16, highlight bool) {
	switch {
	case r == 0:
	case r == menuColor+0xFF:
		if highlight {
			a.setHighlight(0, true)
		} else {
			a.setTextColor(0, true)
		}
	case r == menuColorMore:
		if c, ok := chooseColor(a.wnd.Hwnd(), a.textColor); ok {
			a.setTextColor(c, false)
		}
	case r >= menuColor && r < menuColor+0x100:
		pal := palette16
		if highlight {
			pal = highlightPalette
		}
		if i := int(r - menuColor); i < len(pal) {
			if highlight {
				a.setHighlight(pal[i], false)
			} else {
				a.setTextColor(pal[i], false)
			}
		}
	case r >= menuList && r < menuList+16:
		a.setList(r - menuList)
	case r == menuSpaceAfter:
		cur := a.re.ParaFormat()
		pf := newParaFormat()
		pf.Mask = pfmSpaceAfter
		if cur.SpaceAfter < 200 {
			pf.SpaceAfter = 200
		}
		a.re.SetParaFormat(&pf)
	case r >= menuSpacing && int(r-menuSpacing) < len(lineSpacings):
		a.setLineSpacing(lineSpacings[r-menuSpacing])
	case r >= menuTable && r < menuTable+0x100:
		n := int(r - menuTable)
		a.insertTable(n/16, n%16)
	case r >= menuWrap && r <= menuWrap+config.WrapRuler:
		a.cfg.Wrap = int(r - menuWrap)
		a.applyWrap()
		a.updateRuler()
	case r == menuUnits || r == menuUnits+1:
		a.cfg.Units = "in"
		if r == menuUnits+1 {
			a.cfg.Units = "cm"
		}
		a.updateRuler()
	default:
		a.command(r)
	}
}

func (a *App) listMenu() win.HMENU {
	m, _ := win.CreatePopupMenu()
	cur := a.re.ParaFormat()
	kinds := []struct {
		kind  uint16
		label string
	}{{0, "&None"}, {pfnBullet, "&Bullets •"}, {pfnArabic, "&Numbers 1. 2. 3."}, {pfnLCLetter, "&Lowercase letters a. b. c."},
		{pfnUCLetter, "&Uppercase letters A. B. C."}, {pfnLCRoman, "Lowercase &roman i. ii. iii."}, {pfnUCRoman, "Uppercase r&oman I. II. III."}}
	for _, k := range kinds {
		appendMenu(m, mfString, uintptr(menuList+int(k.kind)), k.label)
	}
	m.CheckMenuItemByCmd(true, uint16(menuList+int(cur.Numbering)))
	return m
}

var lineSpacings = []float64{1, 1.15, 1.5, 2}

func (a *App) spacingMenu() win.HMENU {
	m, _ := win.CreatePopupMenu()
	cur := a.re.ParaFormat()
	curLines := 1.0
	switch cur.LineRule {
	case 5:
		curLines = float64(cur.LineSpacing) / 20
	case 1:
		curLines = 1.5
	case 2:
		curLines = 2
	}
	for i, s := range lineSpacings {
		appendMenu(m, mfString, uintptr(menuSpacing+i), strconv.FormatFloat(s, 'f', -1, 64))
		if s == curLines {
			m.CheckMenuItemByCmd(true, uint16(menuSpacing+i))
		}
	}
	appendMenu(m, mfSeparator, 0, "")
	appendMenu(m, mfString, menuSpaceAfter, "Add 10pt space after paragraphs")
	m.CheckMenuItemByCmd(cur.SpaceAfter >= 200, menuSpaceAfter)
	return m
}

func (a *App) tableMenu() win.HMENU {
	m, _ := win.CreatePopupMenu()
	for rows := 2; rows <= 6; rows++ {
		for cols := 2; cols <= 5; cols++ {
			if rows == cols || rows == cols+1 || cols == 2 || rows == 2 {
				appendMenu(m, mfString, uintptr(menuTable+rows*16+cols), fmt.Sprintf("%d columns × %d rows", cols, rows))
			}
		}
	}
	return m
}

func (a *App) wrapMenu() win.HMENU {
	m, _ := win.CreatePopupMenu()
	appendMenu(m, mfString, menuWrap+config.WrapNone, "&No wrap")
	appendMenu(m, mfString, menuWrap+config.WrapWindow, "Wrap to &window")
	appendMenu(m, mfString, menuWrap+config.WrapRuler, "Wrap to &ruler")
	m.CheckMenuItemByCmd(true, uint16(menuWrap+a.cfg.Wrap))
	return m
}

func (a *App) unitsMenu() win.HMENU {
	m, _ := win.CreatePopupMenu()
	appendMenu(m, mfString, menuUnits, "&Inches")
	appendMenu(m, mfString, menuUnits+1, "&Centimeters")
	cm := a.cfg.Units == "cm"
	m.CheckMenuItemByCmd(!cm, menuUnits)
	m.CheckMenuItemByCmd(cm, menuUnits+1)
	return m
}

// menuBar shows the menu of a button of the menu row.
func (a *App) menuBar(index int, at win.POINT) {
	defer a.re.Hwnd().SetFocus()
	m, _ := win.CreatePopupMenu()
	defer m.DestroyMenu() // destroys the submenus too
	items := func(m win.HMENU, list ...any) {
		for i := 0; i < len(list); i++ {
			switch v := list[i].(type) {
			case nil:
				appendMenu(m, mfSeparator, 0, "")
			case uint16:
				i++
				appendMenu(m, mfString, uintptr(v), list[i].(string))
			case win.HMENU:
				i++
				appendMenu(m, mfPopup, uintptr(v), list[i].(string))
			}
		}
	}
	sub := func(list ...any) win.HMENU {
		s, _ := win.CreatePopupMenu()
		items(s, list...)
		return s
	}
	cf := a.re.CharFormat()
	pf := a.re.ParaFormat()
	switch index {
	case 0: // File
		saveAs := sub(cmdSaveRTF, "&Rich Text document (.rtf)", cmdSaveMarkdown, "&Markdown document (.md)",
			cmdSaveHTML, "&HTML document (.html)", cmdSaveText, "&Plain text document (.txt)", nil,
			cmdExportPDF, "P&DF document (.pdf)")
		items(m, cmdNew, "&New\tCtrl+N", cmdNewWindow, "New &window\tCtrl+Shift+N", cmdOpen, "&Open...\tCtrl+O",
			cmdSave, "&Save\tCtrl+S", saveAs, "Save &as", cmdExportPDF, "&Export to PDF...", nil,
			cmdPrint, "&Print...\tCtrl+P", cmdQuickPrint, "&Quick print", cmdPreview, "Print pre&view\tCtrl+F2",
			cmdPageSetup, "Page set&up...")
		if len(a.cfg.Recent) > 0 {
			recent, _ := win.CreatePopupMenu()
			for i, p := range a.cfg.Recent {
				appendMenu(recent, mfString, uintptr(cmdRecent+uint16(i)), fmt.Sprintf("&%d %s", i+1, strings.ReplaceAll(p, "&", "&&")))
			}
			items(m, nil, recent, "&Recent documents")
		}
		items(m, nil, cmdExit, "E&xit")
	case 1: // Edit (File is 0)
		items(m, cmdUndo, "&Undo\tCtrl+Z", cmdRedo, "&Redo\tCtrl+Y", nil, cmdCut, "Cu&t\tCtrl+X", cmdCopy, "&Copy\tCtrl+C",
			cmdPaste, "&Paste\tCtrl+V", cmdPasteText, "Paste as te&xt\tCtrl+Shift+V", nil, cmdSelectAll, "Select &all\tCtrl+A", nil,
			cmdFind, "&Find...\tCtrl+F", cmdFindNext, "Find &next\tF3", cmdReplace, "R&eplace...\tCtrl+H")
		enableItems(m, a.re.CanUndo(), cmdUndo)
		enableItems(m, a.re.CanRedo(), cmdRedo)
		enableItems(m, a.re.HasSelection(), cmdCut, cmdCopy)
		enableItems(m, a.re.CanPaste(), cmdPaste, cmdPasteText)
	case 3: // Insert
		items(m, cmdPicture, "&Picture...", a.tableMenu(), "&Table", cmdDateTime, "&Date and time...",
			cmdLink, "&Link...\tCtrl+K", cmdRule, "&Horizontal line")
	case 4: // Format
		style := sub(cmdHeading0, "&Normal\tCtrl+Alt+0", cmdHeading1, "Heading &1\tCtrl+Alt+1", cmdHeading2, "Heading &2\tCtrl+Alt+2",
			cmdHeading3, "Heading &3\tCtrl+Alt+3", cmdHeading4, "Heading &4\tCtrl+Alt+4", cmdHeading5, "Heading &5\tCtrl+Alt+5",
			cmdHeading6, "Heading &6\tCtrl+Alt+6", nil, cmdQuote, "&Quote", cmdCode, "&Code")
		align := sub(cmdAlignLeft, "&Left\tCtrl+L", cmdAlignCenter, "&Center\tCtrl+E", cmdAlignRight, "&Right\tCtrl+R", cmdJustify, "&Justify\tCtrl+J")
		colors := a.colorMenu(false)
		items(m, cmdFontDialog, "&Font...\tCtrl+D", cmdParagraph, "&Paragraph...", style, "&Style", nil,
			cmdBold, "&Bold\tCtrl+B", cmdItalic, "&Italic\tCtrl+I", cmdUnderline, "&Underline\tCtrl+U", cmdStrike, "Stri&kethrough",
			cmdSubscript, "Subscrip&t\tCtrl+=", cmdSuperscript, "Su&perscript\tCtrl+Shift+=", cmdGrow, "&Grow font\tCtrl+Shift+>",
			cmdShrink, "Shrin&k font\tCtrl+Shift+<", colors, "Text c&olor", nil,
			align, "&Align", a.listMenu(), "Li&st", a.spacingMenu(), "Line spa&cing", cmdIndent, "Increase in&dent", cmdOutdent, "Decrease ind&ent")
		on := func(mask, effect uint32) bool { return cf.Mask&mask != 0 && cf.Effects&effect != 0 }
		m.CheckMenuItemByCmd(on(cfmBold, cfeBold), cmdBold)
		m.CheckMenuItemByCmd(on(cfmItalic, cfeItalic), cmdItalic)
		m.CheckMenuItemByCmd(on(cfmUnderline, cfeUnderline), cmdUnderline)
		m.CheckMenuItemByCmd(on(cfmStrikeout, cfeStrikeout), cmdStrike)
		m.CheckMenuItemByCmd(cf.Effects&cfeSubscript != 0, cmdSubscript)
		m.CheckMenuItemByCmd(cf.Effects&cfeSuperscript != 0, cmdSuperscript)
		for i, id := range []uint16{cmdAlignLeft, cmdAlignRight, cmdAlignCenter, cmdJustify} {
			align.CheckMenuItemByCmd(pf.Mask&pfmAlignment != 0 && pf.Alignment == uint16(i+1), id)
		}
		st := a.styleOfSelection(&cf, &pf)
		style.CheckMenuItemByCmd(true, []uint16{cmdHeading0, cmdHeading1, cmdHeading2, cmdHeading3, cmdHeading4, cmdHeading5, cmdHeading6, cmdQuote, cmdCode}[st])
	case 2: // View
		items(m, cmdRuler, "&Ruler", cmdStatusBar, "&Status bar", nil, a.wrapMenu(), "&Word wrap", a.unitsMenu(), "&Measurement units", nil,
			cmdPreview, "Print pre&view\tCtrl+F2")
		m.CheckMenuItemByCmd(a.cfg.Ruler, cmdRuler)
		m.CheckMenuItemByCmd(a.cfg.StatusBar, cmdStatusBar)
	case 5: // Help
		items(m, cmdHelp, "&Help topics\tF1", cmdUpdate, "Check for up&dates...", nil, cmdAbout, "&About "+appName)
	}
	a.choose(a.popup(m, at), false)
}

func (a *App) contextMenu(pt win.POINT) {
	if pt.X == -1 && pt.Y == -1 { // from the keyboard: at the caret
		s, _ := a.re.Selection()
		var p win.POINT
		a.re.send(emPosFromChar, uintptr(unsafe.Pointer(&p)), uintptr(s))
		a.re.Hwnd().ClientToScreenPt(&p)
		pt = p
	}
	m, _ := win.CreatePopupMenu()
	defer m.DestroyMenu()
	appendMenu(m, mfString, uintptr(cmdUndo), "&Undo")
	appendMenu(m, mfSeparator, 0, "")
	appendMenu(m, mfString, uintptr(cmdCut), "Cu&t")
	appendMenu(m, mfString, uintptr(cmdCopy), "&Copy")
	appendMenu(m, mfString, uintptr(cmdPaste), "&Paste")
	appendMenu(m, mfString, uintptr(cmdPasteText), "Paste as te&xt")
	appendMenu(m, mfSeparator, 0, "")
	appendMenu(m, mfString, uintptr(cmdSelectAll), "Select &all")
	appendMenu(m, mfSeparator, 0, "")
	appendMenu(m, mfString, uintptr(cmdFontDialog), "&Font...")
	appendMenu(m, mfString, uintptr(cmdParagraph), "Paragrap&h...")
	appendMenu(m, mfString, uintptr(cmdLink), "&Link...")
	enableItems(m, a.re.CanUndo(), cmdUndo)
	enableItems(m, a.re.HasSelection(), cmdCut, cmdCopy)
	enableItems(m, a.re.CanPaste(), cmdPaste, cmdPasteText)
	if r := a.popup(m, pt); r != 0 {
		a.command(r)
	}
}

func enableItems(m win.HMENU, enable bool, ids ...uint16) {
	flags := uintptr(mfGrayed)
	if enable {
		flags = 0
	}
	for _, id := range ids {
		procEnableMenuItem.Call(uintptr(m), uintptr(id), flags)
	}
}

// ---- the combo boxes ----

func (a *App) comboChanged(id, notify uint16) {
	if a.updating {
		return
	}
	h := a.tb.Combo(id)
	if notify != uint16(co.CBN_SELCHANGE) && notify != uint16(co.CBN_SELENDOK) {
		return
	}
	sel := int(int32(send(h, co.CB_GETCURSEL, 0, 0)))
	if sel < 0 {
		return
	}
	text := comboItem(h, sel)
	switch id {
	case comboFont:
		a.setFontFace(text)
	case comboSize:
		if v, err := strconv.ParseFloat(text, 64); err == nil {
			a.setFontSize(v)
		}
	case comboStyle:
		a.applyStyle(sel)
	}
	if notify == uint16(co.CBN_SELENDOK) {
		a.re.Hwnd().SetFocus()
	}
}

func comboItem(h win.HWND, i int) string {
	n := int(int32(send(h, co.CB_GETLBTEXTLEN, uintptr(i), 0)))
	if n <= 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	h.SendMessage(co.CB_GETLBTEXT, win.WPARAM(i), win.LPARAM(unsafe.Pointer(&buf[0])))
	return syscall.UTF16ToString(buf)
}

// comboEnter applies what was typed in the font or size box.
func (a *App) comboEnter() {
	focus, _, _ := procGetFocus.Call()
	for _, id := range []uint16{comboFont, comboSize} {
		c := a.tb.Combo(id)
		if c == 0 || (win.HWND(focus) != c && !c.IsChild(win.HWND(focus))) {
			continue
		}
		text := strings.TrimSpace(windowText(c))
		if id == comboFont {
			a.setFontFace(text)
		} else if v, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64); err == nil {
			a.setFontSize(v)
		} else {
			beep()
		}
		a.re.Hwnd().SetFocus()
		return
	}
}

// ---- links ----

func (a *App) link(p unsafe.Pointer) uintptr {
	// ENLINK, packed to 4 bytes like all of richedit.h: after the NMHDR
	// (24 bytes) come msg, wParam and lParam without padding.
	type enLinkT struct {
		hdr    [24]byte
		msg    uint32
		wParam [2]uint32
		lParam [2]uint32
		chrg   charRange
	}
	el := (*enLinkT)(p)
	switch co.WM(el.msg) {
	case co.WM_LBUTTONUP:
		if !keyDown(co.VK_CONTROL) {
			return 0
		}
		url := a.linkURL(int(el.chrg.Min), int(el.chrg.Max))
		if url != "" {
			shellOpen(a.wnd.Hwnd(), url)
		}
		return 1
	case co.WM_MOUSEMOVE:
		a.sb.SetText("Ctrl+click to follow the link")
		a.wnd.Hwnd().SetTimer(timerWords, 1500)
	case co.WM_SETCURSOR:
		if !keyDown(co.VK_CONTROL) {
			procSetCursor.Call(loadCursor(idcIBeam))
			return 1
		}
	}
	return 0
}

// linkURL finds the address of a link: the hidden address of a friendly
// link, or the text itself.
func (a *App) linkURL(from, to int) string {
	s, e := a.re.Selection()
	a.re.send(emHideSelection, 1, 0)
	a.re.SetSelection(from, to)
	rtf := a.re.RTF(true)
	a.re.SetSelection(s, e)
	a.re.send(emHideSelection, 0, 0)
	if d, err := doc.ReadRTF(rtf); err == nil {
		for _, b := range d.Blocks {
			if p, ok := b.(*doc.Paragraph); ok {
				for _, r := range p.Runs {
					if r.Link != "" {
						return r.Link
					}
				}
			}
		}
	}
	text := strings.TrimSpace(a.re.TextRange(from, to))
	if strings.Contains(text, "://") || strings.HasPrefix(text, "mailto:") || strings.HasPrefix(text, "www.") {
		if strings.HasPrefix(text, "www.") {
			text = "https://" + text
		}
		return text
	}
	return ""
}

// ---- the editor's subclass ----

var (
	editorApp      *App
	editorSubclass = syscall.NewCallback(editorProc)
)

func (a *App) subclassEditor() {
	editorApp = a
	procSetWindowSubclass.Call(uintptr(a.re.Hwnd()), editorSubclass, 0x5752, 0)
}

func editorProc(hwnd, msg, wParam, lParam, id, ref uintptr) uintptr {
	a := editorApp
	switch co.WM(msg) {
	case co.WM_GETDLGCODE:
		// All keys stay in the editor (IsDialogMessage would take Tab and
		// Enter).
		return 0x0004 | 0x0080 | 0x0001 | 0x0002 | 0x0008 // DLGC_WANTALLKEYS | DLGC_WANTCHARS | DLGC_WANTARROWS | DLGC_WANTTAB | DLGC_HASSETSEL
	case wmMouseWheel:
		if keyDown(co.VK_CONTROL) {
			return 0 // RichEdit would zoom
		}
		r, _, _ := procDefSubclassProc.Call(hwnd, msg, wParam, lParam)
		if a != nil {
			a.updateRuler()
		}
		return r
	case co.WM_HSCROLL:
		r, _, _ := procDefSubclassProc.Call(hwnd, msg, wParam, lParam)
		if a != nil {
			a.updateRuler()
		}
		return r
	}
	r, _, _ := procDefSubclassProc.Call(hwnd, msg, wParam, lParam)
	return r
}

// ---- commands ----

func (a *App) newWindow() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe)
	if rc, err := a.wnd.Hwnd().GetWindowRect(); err == nil && !a.wnd.Hwnd().IsZoomed() {
		step := int32(ui.DpiX(26))
		cmd.Env = append(os.Environ(), fmt.Sprintf("WORD4WIN_POS=%d,%d", rc.Left+step, rc.Top+step))
	}
	a.saveSettings()
	if err := cmd.Start(); err != nil {
		a.wnd.Hwnd().MessageBox("Cannot open a new window.\n\n"+err.Error(), appName, co.MB_ICONERROR)
	}
}

func localeFormat(proc *windows.LazyProc, flags uintptr, format string) string {
	var buf [128]uint16
	var f uintptr
	if format != "" {
		f = uintptr(unsafe.Pointer(utf16Ptr(format)))
		flags = 0
	}
	n, _, _ := proc.Call(0x0400, flags, 0, f, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func (a *App) fileError(verb, path string, err error) {
	reason := err.Error()
	if pe, ok := err.(*os.PathError); ok {
		reason = pe.Err.Error()
	}
	a.wnd.Hwnd().MessageBox(fmt.Sprintf("Cannot %s %s.\n\n%s", verb, path, reason), appName, co.MB_ICONEXCLAMATION)
}
