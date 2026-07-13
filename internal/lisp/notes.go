package lisp

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

func NoteNumber(v any) (uint8, error) {
	switch n := v.(type) {
	case int:
		return checkedNote(int64(n))
	case int64:
		return checkedNote(n)
	case string:
		return ParseNoteName(n)
	default:
		return 0, fmt.Errorf("note must be a MIDI number or note name, got %T", v)
	}
}
func checkedNote(n int64) (uint8, error) {
	if n < 0 || n > 127 {
		return 0, fmt.Errorf("MIDI note must be between 0 and 127, got %d", n)
	}
	return uint8(n), nil
}
func ParseNoteName(s string) (uint8, error) {
	original := s
	s = strings.TrimSpace(strings.TrimPrefix(s, ":"))
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid note name %q", original)
	}
	letter := unicode.ToUpper(rune(s[0]))
	base, ok := map[rune]int{'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11}[letter]
	if !ok {
		return 0, fmt.Errorf("invalid note name %q: expected A-G", original)
	}
	i := 1
	if i < len(s) && (s[i] == '#' || s[i] == 'b' || s[i] == 'B') {
		if s[i] == '#' {
			base++
		} else {
			base--
		}
		i++
	}
	if i >= len(s) {
		return 0, fmt.Errorf("invalid note name %q: missing octave", original)
	}
	oct, err := strconv.Atoi(s[i:])
	if err != nil {
		return 0, fmt.Errorf("invalid note name %q: bad octave", original)
	}
	midi := (oct+1)*12 + base
	if midi < 0 || midi > 127 {
		return 0, fmt.Errorf("note %q is outside MIDI range 0-127", original)
	}
	return uint8(midi), nil
}
