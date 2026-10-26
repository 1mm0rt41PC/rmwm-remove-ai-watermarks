package main

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// CF_UNICODETEXT must be NUL-terminated and contain well-formed UTF-16. Reject
// lone surrogates instead of silently replacing them with U+FFFD.
func decodeWindowsClipboardText(words []uint16, maxBytes int64) (string, error) {
	var out strings.Builder
	for i := 0; i < len(words); i++ {
		word := words[i]
		if word == 0 {
			return out.String(), nil
		}
		var ch rune
		switch {
		case word >= 0xD800 && word <= 0xDBFF:
			if i+1 >= len(words) || words[i+1] < 0xDC00 || words[i+1] > 0xDFFF {
				return "", fmt.Errorf("presse-papiers UTF-16 invalide (surrogate incomplet)")
			}
			ch = utf16.DecodeRune(rune(word), rune(words[i+1]))
			i++
		case word >= 0xDC00 && word <= 0xDFFF:
			return "", fmt.Errorf("presse-papiers UTF-16 invalide (surrogate isolé)")
		default:
			ch = rune(word)
		}
		if int64(out.Len())+int64(utf8.RuneLen(ch)) > maxBytes {
			return "", fmt.Errorf("texte du presse-papiers supérieur à la limite autorisée")
		}
		out.WriteRune(ch)
	}
	return "", fmt.Errorf("presse-papiers UTF-16 invalide (terminaison NUL absente)")
}
