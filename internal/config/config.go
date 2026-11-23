// Package config keeps the settings of word4win in
// %APPDATA%\word4win\settings.json.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Word wrap modes.
const (
	WrapNone   = 0
	WrapWindow = 1
	WrapRuler  = 2
)

// Settings is everything that is remembered between runs.
type Settings struct {
	// The window, in pixels; a width of 0 lets Windows choose.
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximized bool `json:"maximized"`

	StatusBar bool   `json:"statusBar"`
	Ruler     bool   `json:"ruler"`
	Wrap      int    `json:"wrap"`
	Units     string `json:"units"` // "in" or "cm"; empty follows Windows

	// Page setup, in points. A paper size of 0 follows Windows (Letter in
	// the US, A4 elsewhere).
	PaperWidth   float64 `json:"paperWidth"`
	PaperHeight  float64 `json:"paperHeight"`
	MarginLeft   float64 `json:"marginLeft"`
	MarginTop    float64 `json:"marginTop"`
	MarginRight  float64 `json:"marginRight"`
	MarginBottom float64 `json:"marginBottom"`
	PrintScale   int     `json:"printScale"` // percent

	MatchCase bool `json:"matchCase"`
	WholeWord bool `json:"wholeWord"`

	Recent []string `json:"recent"`
}

// MaxRecent is the length of the recent documents list.
const MaxRecent = 9

// Default returns the settings of a first run, which are WordPad's.
func Default() *Settings {
	return &Settings{
		StatusBar: true, Ruler: true, Wrap: WrapRuler,
		MarginLeft: 90, MarginRight: 90, MarginTop: 72, MarginBottom: 72,
		PrintScale: 100,
	}
}

// Dir returns the folder of the settings file.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "word4win"), nil
}

// Load reads the settings; without a readable file it returns the defaults.
func Load(dir string) *Settings {
	s := Default()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return s
	}
	if json.Unmarshal(data, s) != nil {
		return Default()
	}
	if s.PrintScale < 10 || s.PrintScale > 400 {
		s.PrintScale = 100
	}
	if s.Wrap < WrapNone || s.Wrap > WrapRuler {
		s.Wrap = WrapRuler
	}
	for _, m := range []*float64{&s.MarginLeft, &s.MarginTop, &s.MarginRight, &s.MarginBottom} {
		if *m < 0 || *m > 288 {
			*m = 72
		}
	}
	if len(s.Recent) > MaxRecent {
		s.Recent = s.Recent[:MaxRecent]
	}
	return s
}

// Save writes the settings.
func (s *Settings) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "settings.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "settings.json"))
}

// AddRecent puts a file at the top of the recent documents.
func (s *Settings) AddRecent(path string) {
	list := []string{path}
	for _, p := range s.Recent {
		if p != path && len(list) < MaxRecent {
			list = append(list, p)
		}
	}
	s.Recent = list
}
