package lisp

import "testing"

func TestParseNoteName(t *testing.T) {
	for s, want := range map[string]uint8{"C4": 60, "c#4": 61, "Db4": 61, "A4": 69, "C-1": 0, "G9": 127} {
		got, err := ParseNoteName(s)
		if err != nil || got != want {
			t.Errorf("%s = %d,%v want %d", s, got, err, want)
		}
	}
	for _, s := range []string{"H9", "C", "C#x", "C10"} {
		if _, err := ParseNoteName(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
func FuzzParseNoteName(f *testing.F) {
	for _, s := range []string{"C4", "Db3", "", "A999"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _, _ = ParseNoteName(s) })
}
