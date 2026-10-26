// Unicode cleaning rules derived from watermarks-remover (MIT):
// Copyright (c) 2026 Guillaume Meyer and contributors.
// https://github.com/guillaumemeyer/watermarks-remover
// Reference commit: 980c0b4c5786ca710ed77276b6e55b622f340d4c.
// See LICENSE for the upstream copyright and permission notice.

package main

import (
	"strings"
	"unicode"
)

// CleanOptions controls the deterministic, local Unicode cleaning pass.
// The CLI enables NormalizeSpaces and NormalizeDashes by default.
// The other options are opt-in.
type CleanOptions struct {
	NormalizeSpaces      bool
	NormalizeDashes      bool
	StripEmojiGlue       bool
	StripBidi            bool
	AggressiveHomoglyphs bool
}

// CleanStats counts Unicode code points, not UTF-8 bytes.
type CleanStats struct {
	Removed     int
	Replaced    int
	InputRunes  int
	OutputRunes int
}

// CleanText removes deterministic Unicode carriers and applies the selected
// space and dash normalization. It does not detect/remove statistical text
// watermarks. Input must be valid UTF-8;
// encoding validation and file format detection belong to the I/O layer.
// Whitespace runs, indentation, line endings, and final newlines are preserved.
func CleanText(text string, opts CleanOptions) (string, CleanStats) {
	ascii := true
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return text, CleanStats{InputRunes: len(text), OutputRunes: len(text)}
	}

	runes := []rune(text)
	stats := CleanStats{InputRunes: len(runes)}
	var validFlags, validEmbeddings []bool
	if !opts.StripEmojiGlue {
		validFlags = flagTagIndices(runes)
	}
	if !opts.StripBidi {
		validEmbeddings = bidiEmbeddingIndices(runes)
	}

	var out strings.Builder
	out.Grow(len(text))
	var prevKept rune
	havePrevKept := false
	for i, ch := range runes {
		var prevInput, nextInput rune
		havePrevInput, haveNextInput := i > 0, i+1 < len(runes)
		if havePrevInput {
			prevInput = runes[i-1]
		}
		if haveNextInput {
			nextInput = runes[i+1]
		}
		validFlag := len(validFlags) != 0 && validFlags[i]
		validEmbedding := len(validEmbeddings) != 0 && validEmbeddings[i]
		action, replacement := decideRune(ch, prevKept, prevInput, nextInput,
			havePrevKept, havePrevInput, haveNextInput,
			validFlag, validEmbedding, opts)
		switch action {
		case actionStrip:
			stats.Removed++
		case actionReplace:
			out.WriteRune(replacement)
			stats.Replaced++
			stats.OutputRunes++
			prevKept, havePrevKept = replacement, true
		default:
			out.WriteRune(ch)
			stats.OutputRunes++
			// Emoji/script glue stays attached to the previous visible base.
			// Advancing this state on VS16 would break chains such as heart/fire.
			if !isGlueRune(ch) {
				prevKept, havePrevKept = ch, true
			}
		}
	}
	if stats.Removed == 0 && stats.Replaced == 0 {
		return text, stats
	}
	return out.String(), stats
}

type runeAction uint8

const (
	actionKeep runeAction = iota
	actionStrip
	actionReplace
)

func decideRune(ch, prevKept, prevInput, nextInput rune,
	havePrevKept, havePrevInput, haveNextInput bool,
	validFlag, validEmbedding bool, opts CleanOptions,
) (runeAction, rune) {
	if !opts.StripBidi && (validEmbedding || isPreservableBidi(ch)) {
		return actionKeep, ch
	}
	if !opts.StripEmojiGlue {
		if havePrevInput && isCJKIdeograph(prevInput) &&
			(isSupplementarySelector(ch) || ch >= 0xFE00 && ch <= 0xFE0D) {
			return actionKeep, ch
		}
		if (ch == 0xFE0E || ch == 0xFE0F) && havePrevInput && isEmojiBase(prevInput) {
			return actionKeep, ch
		}
		if ch == 0x200D && havePrevKept && haveNextInput &&
			isEmojiBase(prevKept) && isEmojiBase(nextInput) {
			return actionKeep, ch
		}
		if (ch == 0x200C || ch == 0x200D) && havePrevInput && haveNextInput {
			prevScript := joiningScript(prevInput)
			if prevScript != 0 && prevScript == joiningScript(nextInput) {
				return actionKeep, ch
			}
		}
		if ch >= 0xE0020 && ch <= 0xE007F && validFlag {
			return actionKeep, ch
		}
		// A Mongolian selector must have a retained Mongolian letter as its
		// base. Upstream also accepts a preceding raw selector/separator,
		// making floating selector chains need multiple cleaning passes.
		// Using the retained letter preserves real chains and is idempotent.
		if isMongolianSelector(ch) && havePrevKept && isMongolianLetter(prevKept) {
			return actionKeep, ch
		}
		if (ch == 0x17B4 || ch == 0x17B5) && havePrevKept &&
			prevKept >= 0x1780 && prevKept <= 0x17FF && unicode.IsLetter(prevKept) {
			return actionKeep, ch
		}
		if isHangulFiller(ch) && havePrevKept && isHangulJamo(prevKept) {
			return actionKeep, ch
		}
		if isOrthographicFormat(ch) {
			return actionKeep, ch
		}
		if low, high, ok := layoutScript(ch); ok &&
			(havePrevInput && prevInput >= low && prevInput <= high ||
				haveNextInput && nextInput >= low && nextInput <= high) {
			return actionKeep, ch
		}
	}
	if isStripRune(ch) {
		return actionStrip, 0
	}
	if opts.NormalizeSpaces && isExoticSpace(ch) {
		return actionReplace, ' '
	}
	// Normalize exactly the requested em dash variants, one code point to one
	// ASCII hyphen. Keep other punctuation, including en dash and minus, intact.
	if opts.NormalizeDashes && (ch == 0x2014 || ch == 0x2E3A || ch == 0x2E3B) {
		return actionReplace, '-'
	}
	if opts.AggressiveHomoglyphs {
		if replacement, ok := latinConfusable(ch); ok {
			return actionReplace, replacement
		}
	}
	if unicode.Is(unicode.Cf, ch) && !isExoticSpace(ch) {
		return actionStrip, 0
	}
	return actionKeep, ch
}

func isStripRune(ch rune) bool {
	switch ch {
	case 0x00AD, 0x034F, 0x061C, 0x115F, 0x1160, 0x17B4, 0x17B5,
		0x180B, 0x180C, 0x180D, 0x180E, 0x180F,
		0x200B, 0x200C, 0x200D, 0x200E, 0x200F,
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E,
		0x2060, 0x2061, 0x2062, 0x2063, 0x2064,
		0x2066, 0x2067, 0x2068, 0x2069,
		0x206A, 0x206B, 0x206C, 0x206D, 0x206E, 0x206F,
		0xFEFF, 0x3164, 0xFFA0, 0xFFF9, 0xFFFA, 0xFFFB:
		return true
	}
	return ch >= 0xFE00 && ch <= 0xFE0F ||
		isSupplementarySelector(ch) ||
		ch >= 0xE0001 && ch <= 0xE007F ||
		isNoncharacter(ch) || isReservedIgnorable(ch) || isPrivateUse(ch)
}

func isExoticSpace(ch rune) bool {
	return ch == 0x00A0 || ch == 0x1680 ||
		ch >= 0x2000 && ch <= 0x200A ||
		ch == 0x202F || ch == 0x205F || ch == 0x3000
}

func latinConfusable(ch rune) (rune, bool) {
	if ch >= 0xFF21 && ch <= 0xFF3A {
		return 'A' + ch - 0xFF21, true
	}
	if ch >= 0xFF41 && ch <= 0xFF5A {
		return 'a' + ch - 0xFF41, true
	}
	switch ch {
	case 0x0410:
		return 'A', true
	case 0x0412:
		return 'B', true
	case 0x0415:
		return 'E', true
	case 0x041A:
		return 'K', true
	case 0x041C:
		return 'M', true
	case 0x041D:
		return 'H', true
	case 0x041E:
		return 'O', true
	case 0x0420:
		return 'P', true
	case 0x0421:
		return 'C', true
	case 0x0422:
		return 'T', true
	case 0x0425:
		return 'X', true
	case 0x0430:
		return 'a', true
	case 0x0435:
		return 'e', true
	case 0x043E:
		return 'o', true
	case 0x0440:
		return 'p', true
	case 0x0441:
		return 'c', true
	case 0x0443:
		return 'y', true
	case 0x0445:
		return 'x', true
	case 0x0456:
		return 'i', true
	}
	return 0, false
}

func isSupplementarySelector(ch rune) bool {
	return ch >= 0xE0100 && ch <= 0xE01EF
}

func isReservedIgnorable(ch rune) bool {
	return ch == 0x2065 || ch == 0xE0000 ||
		ch >= 0xFFF0 && ch <= 0xFFF8 ||
		ch >= 0xE0080 && ch <= 0xE00FF ||
		ch >= 0xE01F0 && ch <= 0xE0FFF
}

func isNoncharacter(ch rune) bool {
	return ch >= 0xFDD0 && ch <= 0xFDEF || ch&0xFFFE == 0xFFFE
}

func isPrivateUse(ch rune) bool {
	return ch >= 0xE000 && ch <= 0xF8FF ||
		ch >= 0xF0000 && ch <= 0xFFFFD ||
		ch >= 0x100000 && ch <= 0x10FFFD
}

func isPreservableBidi(ch rune) bool {
	switch ch {
	case 0x061C, 0x200E, 0x200F, 0x2066, 0x2067, 0x2068, 0x2069:
		return true
	}
	return false
}

func isEmojiBase(ch rune) bool {
	if ch >= 0x1F000 && ch <= 0x1FAFF ||
		ch >= 0x2190 && ch <= 0x25FF ||
		ch >= 0x2600 && ch <= 0x27BF ||
		ch >= 0x2B00 && ch <= 0x2BFF {
		return true
	}
	switch ch {
	case 0x203C, 0x2049, 0x2139, 0x2934, 0x2935,
		0x00A9, 0x00AE, 0x2122, 0x3030, 0x303D, 0x3297, 0x3299,
		'#', '*':
		return true
	}
	return ch >= '0' && ch <= '9'
}

func joiningScript(ch rune) int {
	if !unicode.IsLetter(ch) && !unicode.IsMark(ch) {
		return 0
	}
	switch {
	case ch >= 0x0600 && ch <= 0x08FF:
		return 1 // Arabic / Syriac.
	case ch >= 0x0900 && ch <= 0x0DFF:
		return 2 // Indic.
	case ch >= 0x0F00 && ch <= 0x109F:
		return 3 // Tibetan / Myanmar.
	case ch >= 0x1780 && ch <= 0x17FF:
		return 4 // Khmer.
	case ch >= 0x1800 && ch <= 0x18AF:
		return 5 // Mongolian.
	}
	return 0
}

func isCJKIdeograph(ch rune) bool {
	return ch >= 0x3400 && ch <= 0x4DBF ||
		ch >= 0x4E00 && ch <= 0x9FFF ||
		ch >= 0xF900 && ch <= 0xFAFF ||
		ch >= 0x20000 && ch <= 0x323AF
}

func isMongolianSelector(ch rune) bool {
	return ch == 0x180B || ch == 0x180C || ch == 0x180D || ch == 0x180F
}

func isMongolianLetter(ch rune) bool {
	return ch >= 0x1800 && ch <= 0x18AF && unicode.IsLetter(ch)
}

func isHangulFiller(ch rune) bool {
	return ch == 0x115F || ch == 0x1160 || ch == 0x3164 || ch == 0xFFA0
}

func isHangulJamo(ch rune) bool {
	return ch >= 0x1100 && ch <= 0x11FF ||
		ch >= 0xA960 && ch <= 0xA97C ||
		ch >= 0xD7B0 && ch <= 0xD7C6 ||
		ch >= 0x3131 && ch <= 0x318E ||
		ch >= 0xFFA1 && ch <= 0xFFDC
}

func isOrthographicFormat(ch rune) bool {
	switch ch {
	case 0x0600, 0x0601, 0x0602, 0x0603, 0x0604, 0x0605,
		0x06DD, 0x070F, 0x08E2, 0x110BD, 0x110CD:
		return true
	}
	return false
}

func layoutScript(ch rune) (low, high rune, ok bool) {
	switch {
	case ch >= 0x13430 && ch <= 0x1343F:
		return 0x13000, 0x143FF, true
	case ch >= 0x1BCA0 && ch <= 0x1BCA3:
		return 0x1BC00, 0x1BCA3, true
	case ch >= 0x1D173 && ch <= 0x1D17A:
		return 0x1D100, 0x1D1FF, true
	}
	return 0, 0, false
}

func isGlueRune(ch rune) bool {
	return ch == 0x200C || ch == 0x200D ||
		ch >= 0xFE00 && ch <= 0xFE0F || isSupplementarySelector(ch) ||
		ch >= 0xE0020 && ch <= 0xE007F ||
		isMongolianSelector(ch) || ch == 0x17B4 || ch == 0x17B5 || isHangulFiller(ch)
}

// flagTagIndices accepts only complete black-flag/tag/CANCEL TAG sequences.
func flagTagIndices(text []rune) []bool {
	var valid []bool
	for i := 0; i < len(text); i++ {
		if text[i] != 0x1F3F4 {
			continue
		}
		j := i + 1
		for j < len(text) && text[j] >= 0xE0020 && text[j] <= 0xE007E {
			j++
		}
		if j > i+1 && j < len(text) && text[j] == 0xE007F {
			if valid == nil {
				valid = make([]bool, len(text))
			}
			for k := i + 1; k <= j; k++ {
				valid[k] = true
			}
			i = j
		}
	}
	return valid
}

// bidiEmbeddingIndices preserves paired LRE/RLE embeddings, while LRO/RLO
// overrides and their own PDF terminators remain strip-class.
func bidiEmbeddingIndices(text []rune) []bool {
	var valid []bool
	var stack []int
	for i, ch := range text {
		switch ch {
		case 0x202A, 0x202B, 0x202D, 0x202E:
			stack = append(stack, i)
		case 0x202C:
			if len(stack) == 0 {
				continue
			}
			opener := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if text[opener] == 0x202A || text[opener] == 0x202B {
				if valid == nil {
					valid = make([]bool, len(text))
				}
				valid[opener], valid[i] = true, true
			}
		}
	}
	return valid
}
