//go:build windows

package ui

// The RichEdit control (RICHEDIT50W from Msftedit.dll), which is what
// WordPad edits with. It reads and writes RTF itself; everything else goes
// through the document model of internal/doc.

import (
	"bytes"
	"sync"
	"syscall"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
)

const (
	emGetSel          = 0x00B0
	emSetSel          = 0x00B1
	emScrollCaret     = 0x00B7
	emGetModify       = 0x00B8
	emSetModify       = 0x00B9
	emGetLineCount    = 0x00BA
	emReplaceSel      = 0x00C2
	emCanUndo         = 0x00C6
	emUndo            = 0x00C7
	emSetMargins      = 0x00D3
	emPosFromChar     = 0x00D6
	emCharFromPos     = 0x00D7
	emCanPaste        = wmUser + 50
	emExGetSel        = wmUser + 52
	emExLimitText     = wmUser + 53
	emExSetSel        = wmUser + 55
	emGetCharFormat   = wmUser + 58
	emGetParaFormat   = wmUser + 61
	emGetSelText      = wmUser + 62
	emHideSelection   = wmUser + 63
	emPasteSpecial    = wmUser + 64
	emSetBkgndColor   = wmUser + 67
	emSetCharFormat   = wmUser + 68
	emSetEventMask    = wmUser + 69
	emSetParaFormat   = wmUser + 71
	emSetTargetDevice = wmUser + 72
	emStreamIn        = wmUser + 73
	emStreamOut       = wmUser + 74
	emGetTextRange    = wmUser + 75
	emSetOptions      = wmUser + 77
	emSetUndoLimit    = wmUser + 82
	emRedo            = wmUser + 84
	emCanRedo         = wmUser + 85
	emSetTextMode     = wmUser + 89
	emAutoURLDetect   = wmUser + 91
	emGetTextLengthEx = wmUser + 95
	emSetTextEx       = wmUser + 97
	emFindTextExW     = wmUser + 124
	emSetTypography   = wmUser + 202
	emSetEditStyle    = wmUser + 204
	emGetScrollPos    = wmUser + 221
	emSetScrollPos    = wmUser + 222

	enChange    = 0x0300
	enSelChange = 0x0702
	enLink      = 0x070B
	enHScroll   = 0x0601
	enVScroll   = 0x0602

	enmChange    = 0x00000001
	enmUpdate    = 0x00000002
	enmScroll    = 0x00000004
	enmSelChange = 0x00080000
	enmLink      = 0x04000000

	cfmBold        = 0x00000001
	cfmItalic      = 0x00000002
	cfmUnderline   = 0x00000004
	cfmStrikeout   = 0x00000008
	cfmLink        = 0x00000020
	cfmSuperscript = 0x00030000 // with CFM_SUBSCRIPT
	cfmBackColor   = 0x04000000
	cfmCharset     = 0x08000000
	cfmOffset      = 0x10000000
	cfmFace        = 0x20000000
	cfmColor       = 0x40000000
	cfmSize        = 0x80000000

	cfeBold          = 0x0001
	cfeItalic        = 0x0002
	cfeUnderline     = 0x0004
	cfeStrikeout     = 0x0008
	cfeLink          = 0x0020
	cfeSubscript     = 0x00010000
	cfeSuperscript   = 0x00020000
	cfeAutoBackColor = 0x04000000
	cfeAutoColor     = 0x40000000

	scfSelection = 0x0001
	scfAll       = 0x0004

	pfmStartIndent  = 0x00000001
	pfmRightIndent  = 0x00000002
	pfmOffset       = 0x00000004
	pfmAlignment    = 0x00000008
	pfmNumbering    = 0x00000020
	pfmSpaceBefore  = 0x00000040
	pfmSpaceAfter   = 0x00000080
	pfmLineSpacing  = 0x00000100
	pfmNumStyle     = 0x00002000
	pfmNumTab       = 0x00004000
	pfmNumStart     = 0x00008000
	pfmOutlineLevel = 0x02000000
	pfmOffsetIndent = 0x80000000

	pfnBullet   = 1
	pfnArabic   = 2
	pfnLCLetter = 3
	pfnUCLetter = 4
	pfnLCRoman  = 5
	pfnUCRoman  = 6
	pfnsPeriod  = 0x0200

	pfaLeft    = 1
	pfaRight   = 2
	pfaCenter  = 3
	pfaJustify = 4

	sfText       = 0x0001
	sfRTF        = 0x0002
	sfUnicode    = 0x0010
	sffSelection = 0x8000

	tmRichText           = 2
	tmMultiLevelUndo     = 8
	tmMultiCodepage      = 32
	ecoSelectionBar      = 0x01000000
	ecoopOr              = 2
	toAdvancedTypography = 1

	frDown      = 1
	frWholeWord = 2
	frMatchCase = 4
)

// charFormat is CHARFORMAT2W.
type charFormat struct {
	Size        uint32
	Mask        uint32
	Effects     uint32
	Height      int32 // twips
	Offset      int32
	TextColor   uint32
	CharSet     byte
	Pitch       byte
	FaceName    [32]uint16
	Weight      uint16
	Spacing     int16
	BackColor   uint32
	Lcid        uint32
	Reserved    uint32
	Style       int16
	Kerning     uint16
	Underline   byte
	Animation   byte
	RevAuthor   byte
	UnderlineCl byte
}

func newCharFormat() charFormat {
	var cf charFormat
	cf.Size = uint32(unsafe.Sizeof(cf))
	return cf
}

func (cf *charFormat) face() string { return syscall.UTF16ToString(cf.FaceName[:]) }

func (cf *charFormat) setFace(name string) {
	cf.FaceName = [32]uint16{}
	u, _ := syscall.UTF16FromString(name)
	copy(cf.FaceName[:31], u)
	cf.Mask |= cfmFace
}

// paraFormat is PARAFORMAT2.
type paraFormat struct {
	Size           uint32
	Mask           uint32
	Numbering      uint16
	Effects        uint16
	StartIndent    int32
	RightIndent    int32
	Offset         int32
	Alignment      uint16
	TabCount       int16
	Tabs           [32]int32
	SpaceBefore    int32
	SpaceAfter     int32
	LineSpacing    int32
	Style          int16
	LineRule       byte
	OutlineLevel   byte
	ShadingWeight  uint16
	ShadingStyle   uint16
	NumberingStart uint16
	NumberingStyle uint16
	NumberingTab   uint16
	BorderSpace    uint16
	BorderWidth    uint16
	Borders        uint16
}

func newParaFormat() paraFormat {
	var pf paraFormat
	pf.Size = uint32(unsafe.Sizeof(pf))
	return pf
}

type charRange struct{ Min, Max int32 }

// RichEdit wraps the control.
type RichEdit struct {
	hwnd win.HWND
	id   uint16
}

var loadMsftedit sync.Once

const richEditID = 1001

// newRichEdit creates the control as a child of parent.
func newRichEdit(parent win.HWND) *RichEdit {
	loadMsftedit.Do(func() {
		procLoadLibraryW.Call(uintptr(unsafe.Pointer(utf16Ptr("Msftedit.dll"))))
	})
	hInst, _ := parent.HInstance()
	style := uintptr(co.WS_CHILD|co.WS_VISIBLE|co.WS_VSCROLL|co.WS_HSCROLL|co.WS_CLIPSIBLINGS) |
		0x0004 | 0x0040 | 0x0100 | 0x1000 | 0x8000 // ES_MULTILINE | ES_AUTOVSCROLL | ES_NOHIDESEL | ES_WANTRETURN | ES_SAVESEL
	h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("RICHEDIT50W"))), 0, style,
		0, 0, 100, 100, uintptr(parent), richEditID, uintptr(hInst), 0)
	re := &RichEdit{hwnd: win.HWND(h), id: richEditID}
	re.send(emSetTextMode, tmRichText|tmMultiLevelUndo|tmMultiCodepage, 0)
	re.send(emExLimitText, 0, 0x7FFFFFFE)
	re.send(emSetUndoLimit, 1000, 0)
	re.send(emSetEventMask, 0, enmChange|enmSelChange|enmLink|enmScroll)
	re.send(emAutoURLDetect, 1, 0)
	re.send(emSetOptions, ecoopOr, ecoSelectionBar)
	re.send(emSetTypography, toAdvancedTypography, toAdvancedTypography) // justified text
	return re
}

func (re *RichEdit) Hwnd() win.HWND { return re.hwnd }

func (re *RichEdit) send(msg uint32, w, l uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(uintptr(re.hwnd), uintptr(msg), w, l)
	return r
}

// ---- streaming ----

type streamBuf struct {
	data []byte
	pos  int
	out  bytes.Buffer
}

var (
	streams     = map[uintptr]*streamBuf{}
	streamSeq   uintptr
	streamInCb  = syscall.NewCallback(streamIn)
	streamOutCb = syscall.NewCallback(streamOut)
)

// streamIn and streamOut are EDITSTREAMCALLBACK: DWORD (DWORD_PTR cookie,
// LPBYTE buf, LONG cb, LONG *pcb). All arguments are taken as uintptr.
func streamIn(cookie, buf, cb, pcb uintptr) uintptr {
	n32 := int32(uint32(cb))
	out := *(**int32)(unsafe.Pointer(&pcb))
	s := streams[cookie]
	if s == nil || n32 < 0 {
		*out = 0
		return 1
	}
	n := copy(unsafe.Slice(*(**byte)(unsafe.Pointer(&buf)), int(n32)), s.data[s.pos:])
	s.pos += n
	*out = int32(n)
	return 0
}

func streamOut(cookie, buf, cb, pcb uintptr) uintptr {
	n32 := int32(uint32(cb))
	out := *(**int32)(unsafe.Pointer(&pcb))
	s := streams[cookie]
	if s == nil || n32 < 0 {
		return 1
	}
	s.out.Write(unsafe.Slice(*(**byte)(unsafe.Pointer(&buf)), int(n32)))
	*out = n32
	return 0
}

// editStream is EDITSTREAM. richedit.h packs its structures to 4 bytes,
// so on 64-bit Windows the callback pointer is at offset 12.
type editStream struct {
	cookie     uintptr
	err        uint32
	cbLo, cbHi uint32
}

func (re *RichEdit) stream(msg uint32, format uintptr, s *streamBuf, cb uintptr) uint32 {
	streamSeq++
	id := streamSeq
	streams[id] = s
	defer delete(streams, id)
	es := &editStream{cookie: id, cbLo: uint32(cb), cbHi: uint32(uint64(cb) >> 32)}
	re.send(msg, format, uintptr(unsafe.Pointer(es)))
	return es.err
}

// SetRTF replaces the content with RTF; with selection it replaces the
// selection.
func (re *RichEdit) SetRTF(rtf []byte, selection bool) {
	format := uintptr(sfRTF)
	if selection {
		format |= sffSelection
	}
	re.stream(emStreamIn, format, &streamBuf{data: rtf}, streamInCb)
}

// RTF returns the content (or the selection) as RTF.
func (re *RichEdit) RTF(selection bool) []byte {
	format := uintptr(sfRTF)
	if selection {
		format |= sffSelection
	}
	s := &streamBuf{}
	re.stream(emStreamOut, format, s, streamOutCb)
	return bytes.TrimRight(s.out.Bytes(), "\x00")
}

// Text returns the plain text, with \r as paragraph ends.
func (re *RichEdit) Text() string {
	s := &streamBuf{}
	re.stream(emStreamOut, sfText|sfUnicode, s, streamOutCb)
	b := s.out.Bytes()
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return syscall.UTF16ToString(u)
}

// TextLength is the number of characters.
func (re *RichEdit) TextLength() int {
	gtl := struct{ flags, codepage uint32 }{flags: 0x0008, codepage: 1200} // GTL_NUMCHARS, Unicode
	return int(re.send(emGetTextLengthEx, uintptr(unsafe.Pointer(&gtl)), 0))
}

// ---- selection ----

func (re *RichEdit) Selection() (int, int) {
	var cr charRange
	re.send(emExGetSel, 0, uintptr(unsafe.Pointer(&cr)))
	return int(cr.Min), int(cr.Max)
}

func (re *RichEdit) SetSelection(a, b int) {
	cr := charRange{int32(a), int32(b)}
	re.send(emExSetSel, 0, uintptr(unsafe.Pointer(&cr)))
}

func (re *RichEdit) HasSelection() bool {
	a, b := re.Selection()
	return a != b
}

// ReplaceSelection replaces the selection with text, as one undo step.
func (re *RichEdit) ReplaceSelection(text string) {
	re.send(emReplaceSel, 1, uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func (re *RichEdit) TextRange(a, b int) string {
	if b <= a {
		return ""
	}
	buf := make([]uint16, b-a+1)
	tr := struct {
		cr   charRange
		text *uint16
	}{charRange{int32(a), int32(b)}, &buf[0]}
	re.send(emGetTextRange, 0, uintptr(unsafe.Pointer(&tr)))
	return syscall.UTF16ToString(buf)
}

// ---- formatting ----

// CharFormat returns the format of the selection; Mask tells which
// properties are the same over all of it.
func (re *RichEdit) CharFormat() charFormat {
	cf := newCharFormat()
	re.send(emGetCharFormat, scfSelection, uintptr(unsafe.Pointer(&cf)))
	return cf
}

func (re *RichEdit) SetCharFormat(cf *charFormat) {
	cf.Size = uint32(unsafe.Sizeof(*cf))
	re.send(emSetCharFormat, scfSelection, uintptr(unsafe.Pointer(cf)))
}

func (re *RichEdit) SetDefaultCharFormat(cf *charFormat) {
	cf.Size = uint32(unsafe.Sizeof(*cf))
	re.send(emSetCharFormat, 0, uintptr(unsafe.Pointer(cf))) // SCF_DEFAULT
}

func (re *RichEdit) ParaFormat() paraFormat {
	pf := newParaFormat()
	re.send(emGetParaFormat, 0, uintptr(unsafe.Pointer(&pf)))
	return pf
}

func (re *RichEdit) SetParaFormat(pf *paraFormat) {
	pf.Size = uint32(unsafe.Sizeof(*pf))
	re.send(emSetParaFormat, 0, uintptr(unsafe.Pointer(pf)))
}

// ---- state ----

func (re *RichEdit) Modified() bool     { return re.send(emGetModify, 0, 0) != 0 }
func (re *RichEdit) SetModified(m bool) { re.send(emSetModify, boolArg(m), 0) }
func (re *RichEdit) CanUndo() bool      { return re.send(emCanUndo, 0, 0) != 0 }
func (re *RichEdit) CanRedo() bool      { return re.send(emCanRedo, 0, 0) != 0 }
func (re *RichEdit) Undo()              { re.send(emUndo, 0, 0) }
func (re *RichEdit) Redo()              { re.send(emRedo, 0, 0) }
func (re *RichEdit) Cut()               { re.send(uint32(co.WM_CUT), 0, 0) }
func (re *RichEdit) Copy()              { re.send(uint32(co.WM_COPY), 0, 0) }
func (re *RichEdit) Paste()             { re.send(uint32(co.WM_PASTE), 0, 0) }
func (re *RichEdit) Clear()             { re.send(uint32(co.WM_CLEAR), 0, 0) }
func (re *RichEdit) CanPaste() bool     { return re.send(emCanPaste, 0, 0) != 0 }
func (re *RichEdit) SelectAll()         { re.SetSelection(0, -1) }
func (re *RichEdit) ScrollCaret()       { re.send(emScrollCaret, 0, 0) }

// PasteText pastes the clipboard as plain text.
func (re *RichEdit) PasteText() { re.send(emPasteSpecial, cfUnicodeText, 0) }

// SetMargins sets the left and right padding in pixels.
func (re *RichEdit) SetMargins(left, right int) {
	re.send(emSetMargins, 0x0001|0x0002, uintptr(uint32(left)|uint32(right)<<16)) // EC_LEFTMARGIN | EC_RIGHTMARGIN
}

// Wrap modes.
const (
	wrapNone = iota
	wrapWindow
	wrapRuler
)

var targetDC uintptr

// SetWrap wraps lines at the window, nowhere, or at a width in twips (the
// text width of the page).
func (re *RichEdit) SetWrap(mode int, widthTwips int) {
	switch mode {
	case wrapNone:
		re.send(emSetTargetDevice, 0, 1)
	case wrapWindow:
		re.send(emSetTargetDevice, 0, 0)
	default:
		if targetDC == 0 {
			targetDC, _, _ = procCreateCompatibleDC.Call(0)
		}
		re.send(emSetTargetDevice, targetDC, uintptr(widthTwips))
	}
}

// Find searches text from a position; it returns the match or -1.
func (re *RichEdit) Find(what string, from, to int, flags uint32) (int, int) {
	ft := struct {
		cr    charRange
		text  *uint16
		found charRange
	}{cr: charRange{int32(from), int32(to)}, text: utf16Ptr(what)}
	r := int32(re.send(emFindTextExW, uintptr(flags), uintptr(unsafe.Pointer(&ft))))
	if r < 0 {
		return -1, -1
	}
	return int(ft.found.Min), int(ft.found.Max)
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}
