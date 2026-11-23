package config

import "testing"

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	s := Default()
	s.Ruler = false
	s.AddRecent(`C:\a.rtf`)
	s.AddRecent(`C:\b.md`)
	s.AddRecent(`C:\a.rtf`)
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	got := Load(dir)
	if got.Ruler || !got.StatusBar || got.Wrap != WrapRuler {
		t.Errorf("loaded %+v", got)
	}
	if len(got.Recent) != 2 || got.Recent[0] != `C:\a.rtf` {
		t.Errorf("recent %v", got.Recent)
	}
}

func TestDefaultsWithoutFile(t *testing.T) {
	if s := Load(t.TempDir()); s.PrintScale != 100 || s.MarginLeft != 90 {
		t.Errorf("defaults %+v", s)
	}
}
