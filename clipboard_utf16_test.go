package main

import "testing"

func TestDecodeWindowsClipboardText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		words []uint16
		max   int64
		want  string
		bad   bool
	}{
		{"empty", []uint16{0}, 1, "", false},
		{"exact ASCII limit", []uint16{'a', 'b', 0}, 2, "ab", false},
		{"UTF8 byte limit", []uint16{'é', 0}, 2, "é", false},
		{"UTF8 over limit", []uint16{'é', 0}, 1, "", true},
		{"surrogate pair", []uint16{0xD83D, 0xDC69, 0}, 4, "👩", false},
		{"pair over limit", []uint16{0xD83D, 0xDC69, 0}, 3, "", true},
		{"line endings", []uint16{'a', '\r', '\n', '\t', 0}, 4, "a\r\n\t", false},
		{"first NUL ends text", []uint16{'a', 0, 0xD800, 'b'}, 1, "a", false},
		{"missing NUL", []uint16{'a'}, 1, "", true},
		{"high surrogate at end", []uint16{0xD800}, 4, "", true},
		{"high surrogate before NUL", []uint16{0xD800, 0}, 4, "", true},
		{"high surrogate before ASCII", []uint16{0xD800, 'a', 0}, 4, "", true},
		{"isolated low surrogate", []uint16{0xDC00, 0}, 4, "", true},
		{"FEFF is text", []uint16{0xFEFF, 'x', 0}, 4, "\uFEFFx", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeWindowsClipboardText(tc.words, tc.max)
			if (err != nil) != tc.bad || got != tc.want {
				t.Fatalf("decode = %q, %v; want %q, error=%t", got, err, tc.want, tc.bad)
			}
		})
	}
}
