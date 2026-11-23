// Command word4win is a WordPad for Windows that also reads and writes
// Markdown and HTML, reads Word and OpenDocument files and exports PDF.
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/mevdschee/word4win/internal/ui"
	"github.com/rodrigocfd/windigo/co"
	wui "github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"
)

// version is set at build time by release.sh (-ldflags -X main.version=...).
var version = "dev"

func main() {
	runtime.LockOSThread() // the Win32 GUI is single-threaded

	defer func() {
		if r := recover(); r != nil {
			wui.MsgError(nil, "word4win", "word4win crashed", fmt.Sprintf("%v\n\n%s", r, debug.Stack()))
			os.Exit(2)
		}
	}()

	// COM is needed for the file dialogs and the RichEdit control.
	win.CoInitializeEx(co.COINIT_APARTMENTTHREADED | co.COINIT_DISABLE_OLE1DDE)
	defer win.CoUninitialize()

	os.Exit(ui.Run(version, parseArgs(os.Args[1:])))
}

// parseArgs reads the command line like WordPad: an optional file, and
// "/p" to print it on the default printer.
func parseArgs(args []string) ui.Options {
	var opts ui.Options
	var words []string
	for _, arg := range args {
		if len(arg) == 2 && (arg[0] == '/' || arg[0] == '-') && len(words) == 0 && strings.EqualFold(arg[1:], "p") {
			opts.Print = true
			continue
		}
		words = append(words, arg)
	}
	opts.Path = strings.Join(words, " ")
	return opts
}
