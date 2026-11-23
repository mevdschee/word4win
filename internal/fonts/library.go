package fonts

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Face is a font in a style. When the font lacks the bold or italic
// variant, the regular one is drawn bold or slanted.
type Face struct {
	Font        *Font
	SynthBold   bool
	SynthItalic bool
}

type entry struct {
	info
	path  string
	index int
}

// Library finds the fonts installed on the system.
type Library struct {
	mu             sync.Mutex
	dirs           []string
	scanned        bool
	entries        []entry
	loaded         map[string]*Font // path#index
	faces          map[string]Face
	fallback       []*Font
	fallbackLoaded bool
}

// NewLibrary makes a library of the fonts in dirs (searched recursively).
func NewLibrary(dirs ...string) *Library {
	return &Library{dirs: dirs, loaded: map[string]*Font{}, faces: map[string]Face{}}
}

var (
	system     *Library
	systemOnce sync.Once
)

// System returns the library of the fonts installed for Windows (or, on
// other systems, in the usual font folders).
func System() *Library {
	systemOnce.Do(func() { system = NewLibrary(SystemDirs()...) })
	return system
}

// SystemDirs are the folders with installed fonts.
func SystemDirs() []string {
	if runtime.GOOS == "windows" {
		var dirs []string
		win := os.Getenv("WINDIR")
		if win == "" {
			win = `C:\Windows`
		}
		dirs = append(dirs, filepath.Join(win, "Fonts"))
		if l := os.Getenv("LOCALAPPDATA"); l != "" {
			dirs = append(dirs, filepath.Join(l, "Microsoft", "Windows", "Fonts"))
		}
		// Under Wine the Windows font folder has few fonts; the Linux ones
		// are on drive Z:.
		if _, err := os.Stat(`Z:\usr\share\fonts`); err == nil {
			dirs = append(dirs, `Z:\usr\share\fonts`, `Z:\usr\local\share\fonts`)
		}
		return dirs
	}
	dirs := []string{"/usr/share/fonts", "/usr/local/share/fonts", "/Library/Fonts", "/System/Library/Fonts"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".fonts"), filepath.Join(home, ".local", "share", "fonts"), filepath.Join(home, "Library", "Fonts"))
	}
	return dirs
}

func (l *Library) scan() {
	if l.scanned {
		return
	}
	l.scanned = true
	for _, dir := range l.dirs {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".ttf", ".ttc":
			default:
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()
			offsets, err := fontOffsets(f)
			if err != nil {
				return nil
			}
			for i, off := range offsets {
				tables, err := readTables(f, off)
				if err != nil {
					continue
				}
				in, err := readInfo(f, tables)
				if err != nil {
					continue
				}
				l.entries = append(l.entries, entry{info: in, path: path, index: i})
			}
			return nil
		})
	}
}

func (l *Library) load(e entry) *Font {
	key := e.path + "#" + strconv.Itoa(e.index)
	if f, ok := l.loaded[key]; ok {
		return f
	}
	l.loaded[key] = nil
	data, err := os.ReadFile(e.path)
	if err != nil {
		return nil
	}
	offsets, err := fontOffsets(bytesReaderAt(data))
	if err != nil || e.index >= len(offsets) {
		return nil
	}
	f, err := parse(data, offsets[e.index])
	if err != nil {
		return nil
	}
	f.Path, f.Index = e.path, e.index
	l.loaded[key] = f
	return f
}

// Substitutes for fonts that may be missing: metric compatible fonts first.
var substitutes = map[string][]string{
	"calibri":         {"Carlito", "Liberation Sans", "Arial"},
	"calibri light":   {"Carlito", "Calibri", "Liberation Sans", "Arial"},
	"cambria":         {"Caladea", "Liberation Serif", "Times New Roman"},
	"arial":           {"Liberation Sans", "Arimo", "Helvetica", "DejaVu Sans"},
	"helvetica":       {"Arial", "Liberation Sans", "Arimo"},
	"times new roman": {"Liberation Serif", "Tinos", "Times", "DejaVu Serif"},
	"times":           {"Times New Roman", "Liberation Serif", "Tinos"},
	"courier new":     {"Liberation Mono", "Cousine", "Courier", "DejaVu Sans Mono"},
	"courier":         {"Courier New", "Liberation Mono", "Cousine"},
	"consolas":        {"Cascadia Mono", "Courier New", "DejaVu Sans Mono", "Liberation Mono", "Noto Mono"},
	"segoe ui":        {"Tahoma", "Noto Sans", "DejaVu Sans", "Arial"},
	"georgia":         {"Gelasio", "Times New Roman", "Liberation Serif"},
	"verdana":         {"DejaVu Sans", "Tahoma", "Arial"},
	"tahoma":          {"DejaVu Sans", "Verdana", "Arial"},
	"liberation sans": {"Arial", "Arimo"},
	"aptos":           {"Calibri", "Carlito", "Arial", "Liberation Sans"},
	"monospace":       {"Consolas", "Courier New", "DejaVu Sans Mono", "Liberation Mono"},
	"sans-serif":      {"Arial", "Liberation Sans", "DejaVu Sans"},
	"serif":           {"Times New Roman", "Liberation Serif", "DejaVu Serif"},
}

// The last resort for any family.
var generic = []string{"Arial", "Liberation Sans", "Segoe UI", "Tahoma", "DejaVu Sans", "Noto Sans", "Verdana"}

// lookup finds an installed family in a style: the exact style first,
// then the regular font of the family with the style made up.
func (l *Library) lookup(family string, bold, italic bool) (Face, bool) {
	var regular, partial *entry
	for i := range l.entries {
		e := &l.entries[i]
		if !strings.EqualFold(e.family, family) && !strings.EqualFold(e.typoFamily, family) {
			continue
		}
		if e.bold == bold && e.italic == italic {
			if f := l.load(*e); f != nil {
				return Face{Font: f}, true
			}
		}
		if !e.bold && !e.italic && strings.EqualFold(e.family, family) {
			regular = e
		}
		if partial == nil && (bold && e.bold && !e.italic || italic && e.italic && !e.bold) {
			partial = e
		}
	}
	if partial != nil {
		if f := l.load(*partial); f != nil {
			return Face{Font: f, SynthBold: bold && !f.Bold, SynthItalic: italic && !f.Italic}, true
		}
	}
	if regular != nil {
		if f := l.load(*regular); f != nil {
			return Face{Font: f, SynthBold: bold, SynthItalic: italic}, true
		}
	}
	return Face{}, false
}

// Face finds a font for a family and style, with substitutes for missing
// families. It returns a zero Face only when no font is installed at all.
func (l *Library) Face(family string, bold, italic bool) Face {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := strings.ToLower(family)
	if bold {
		key += "|b"
	}
	if italic {
		key += "|i"
	}
	if f, ok := l.faces[key]; ok {
		return f
	}
	l.scan()
	try := append([]string{family}, substitutes[strings.ToLower(family)]...)
	try = append(try, generic...)
	var face Face
	for _, name := range try {
		if f, ok := l.lookup(name, bold, italic); ok {
			face = f
			break
		}
	}
	if face.Font == nil {
		for _, e := range l.entries { // anything at all
			if f := l.load(e); f != nil {
				face = Face{Font: f, SynthBold: bold, SynthItalic: italic}
				break
			}
		}
	}
	l.faces[key] = face
	return face
}

// Fonts tried for characters that the chosen font lacks.
var fallbackFamilies = []string{
	"Segoe UI", "Segoe UI Symbol", "Arial Unicode MS", "Nirmala UI", "Ebrima", "Leelawadee UI",
	"Microsoft YaHei", "SimSun", "MS Gothic", "Yu Gothic", "Malgun Gothic", "Gadugi", "Segoe UI Historic",
	"DejaVu Sans", "Noto Sans", "Noto Sans CJK JP", "Noto Sans Symbols", "Noto Sans Symbols2", "Symbola",
	"FreeSerif", "Liberation Sans", "Arial",
}

// Fallback finds an installed font that has a character.
func (l *Library) Fallback(r rune) (*Font, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.scan()
	if !l.fallbackLoaded {
		l.fallbackLoaded = true
		for _, fam := range fallbackFamilies {
			if f, ok := l.lookup(fam, false, false); ok && f.Font != nil {
				l.fallback = append(l.fallback, f.Font)
			}
		}
	}
	for _, f := range l.fallback {
		if f.HasGlyph(r) {
			return f, true
		}
	}
	return nil, false
}

// Families lists the installed font families, for the font list.
func (l *Library) Families() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.scan()
	seen := map[string]bool{}
	var out []string
	for _, e := range l.entries {
		if !seen[strings.ToLower(e.family)] {
			seen[strings.ToLower(e.family)] = true
			out = append(out, e.family)
		}
	}
	return out
}
