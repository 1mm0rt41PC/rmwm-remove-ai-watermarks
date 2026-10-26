package cleaner

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanUnicodeDefault(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		removed  int
		replaced int
	}{
		{"empty", "", "", 0, 0},
		{"ascii", "Hello, world!", "Hello, world!", 0, 0},
		{"prose", "Café — déjà vu; \"été\". A\u030A", "Café - déjà vu; \"été\". A\u030A", 0, 1},
		{"requested dashes", "a—b⸺c⸻d", "a-b-c-d", 0, 3},
		{"dashes preserve whitespace", "\t—  ⸺\r\n⸻\n", "\t-  -\r\n-\n", 0, 3},
		{"other dashes and minus", "a-b–c−d‐e‑f‒g", "a-b–c−d‐e‑f‒g", 0, 0},
		{"line endings", "a\u200Bb\r\n\t  c  \nlast", "ab\r\n\t  c  \nlast", 1, 0},
		{"zero width", "Hello\u200BWorld\u00AD!", "HelloWorld!", 2, 0},
		{"word joiner and middle BOM", "A\u2060B\uFEFFC", "ABC", 2, 0},
		{"spaces", "a\u00A0b\u202Fc\u2003d\u3000e", "a b c d e", 0, 4},
		{"latin joiners", "a\u200Cb\u200Dc\uFE0F", "abc", 3, 0},
		{"family emoji", "Family: 👨\u200D👩\u200D👧", "Family: 👨\u200D👩\u200D👧", 0, 0},
		{"emoji chain", "❤\uFE0F\u200D🔥", "❤\uFE0F\u200D🔥", 0, 0},
		{"emoji modifiers", "👩🏽\u200D💻", "👩🏽\u200D💻", 0, 0},
		{"emoji singletons", "⁉\uFE0F ℹ\uFE0F ↔\uFE0F ©\uFE0F", "⁉\uFE0F ℹ\uFE0F ↔\uFE0F ©\uFE0F", 0, 0},
		{"keycap", "1\uFE0F\u20E3", "1\uFE0F\u20E3", 0, 0},
		{"regional flag", "🇨🇭", "🇨🇭", 0, 0},
		{"subdivision flag", "\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F", "\U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F", 0, 0},
		{"incomplete flag", "\U0001F3F4\U000E0067\U000E0062", "\U0001F3F4", 2, 0},
		{"orphan tags", "a\U000E0067\U000E0062\U000E007Fb", "ab", 3, 0},
		{"Persian", "می\u200Cروم", "می\u200Cروم", 0, 0},
		{"Devanagari", "क्\u200Dष", "क्\u200Dष", 0, 0},
		{"cross-script joiner", "葛\u200CA", "葛A", 1, 0},
		{"orthographic controls", "x\u0600y\u06DDz\u070F\U000110BD", "x\u0600y\u06DDz\u070F\U000110BD", 0, 0},
		{"directional marks", "السعر \u2066123 USD\u2069\u200F\u061C", "السعر \u2066123 USD\u2069\u200F\u061C", 0, 0},
		{"paired embedding", "English \u202Bالعربية\u202C end", "English \u202Bالعربية\u202C end", 0, 0},
		{"override", "abc\u202Edef\u202C", "abcdef", 2, 0},
		{"unpaired embedding", "abc\u202Bdef", "abcdef", 1, 0},
		{"orphan PDF", "abc\u202C", "abc", 1, 0},
		{"nested override", "a\u202Bb\u202Ec\u202Cd\u202Ce", "a\u202Bbcd\u202Ce", 2, 0},
		{"CJK variant", "葛\U000E0100", "葛\U000E0100", 0, 0},
		{"repeated CJK variant", "葛\U000E0100\U000E0101", "葛\U000E0100", 1, 0},
		{"CJK basic selector", "葛\uFE00", "葛\uFE00", 0, 0},
		{"floating selectors", "A\uFE00\U000E0100B", "AB", 2, 0},
		{"Mongolian", "ᠠ\u180B\u180Cᠡ\u180F", "ᠠ\u180B\u180Cᠡ\u180F", 0, 0},
		{"floating Mongolian chain", "a\u180B\u180Cb", "ab", 2, 0},
		{"floating Mongolian separator", "a\u180E\u180Bb", "ab", 2, 0},
		{"Khmer vowels", "ក\u17B4ខ\u17B5", "ក\u17B4ខ\u17B5", 0, 0},
		{"Hangul partial syllable", "\u1100\u115F\u1161", "\u1100\u115F\u1161", 0, 0},
		{"Hangul compatibility", "\u3131\u3164\u314F", "\u3131\u3164\u314F", 0, 0},
		{"Hangul halfwidth", "\uFFA1\uFFA0\uFFC2", "\uFFA1\uFFA0\uFFC2", 0, 0},
		{"floating fillers", "a\u115F\u1160\u3164\uFFA0b", "ab", 4, 0},
		{"private use", "a\uE000b\U000F0000c\U0010FFFD", "abc", 3, 0},
		{"reserved ignorables", "a\u2065\uFFF0\U000E0000\U000E0080\U000E01F0b", "ab", 5, 0},
		{"Egyptian layout", "\U00013079\U00013430\U000130A7", "\U00013079\U00013430\U000130A7", 0, 0},
		{"Egyptian segment", "\U00013437\U00013079\U000130A7\U00013438", "\U00013437\U00013079\U000130A7\U00013438", 0, 0},
		{"Duployan layout", "\U0001BC02\U0001BCA0\U0001BC03", "\U0001BC02\U0001BCA0\U0001BC03", 0, 0},
		{"music layout", "\U0001D158\U0001D165\U0001D173\U0001D158\U0001D165\U0001D174", "\U0001D158\U0001D165\U0001D173\U0001D158\U0001D165\U0001D174", 0, 0},
		{"floating layout", "a\U00013430b\U0001BCA0c\U0001D173d", "abcd", 3, 0},
		{"Cf fallback", "a\u0890b", "ab", 1, 0},
		{"confusables opt in", "pаy ＡＢＣ", "pаy ＡＢＣ", 0, 0},
	}
	opts := CleanOptions{NormalizeSpaces: true, NormalizeDashes: true}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, stats := CleanText(tt.input, opts)
			if got != tt.want {
				t.Fatalf("CleanText(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if stats.Removed != tt.removed || stats.Replaced != tt.replaced {
				t.Errorf("counts = removed %d replaced %d, want %d/%d", stats.Removed, stats.Replaced, tt.removed, tt.replaced)
			}
			if stats.InputRunes != utf8.RuneCountInString(tt.input) || stats.OutputRunes != utf8.RuneCountInString(got) {
				t.Errorf("wrong rune counts: %+v", stats)
			}
			twice, again := CleanText(got, opts)
			if twice != got || again.Removed != 0 || again.Replaced != 0 {
				t.Errorf("cleaning not idempotent: %q -> %q (%+v)", got, twice, again)
			}
		})
	}
}

func TestCleanUnicodeOptions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		opts  CleanOptions
		want  string
	}{
		{"preserve French spaces", "«\u00A0Bonjour\u202F!\u00A0»\u200B", CleanOptions{}, "«\u00A0Bonjour\u202F!\u00A0»"},
		{"preserve dashes", "a—b⸺c⸻d\u200B", CleanOptions{NormalizeSpaces: true}, "a—b⸺c⸻d"},
		{"normalize dashes keep spaces", "a\u00A0—\u202Fb⸺c⸻d", CleanOptions{NormalizeDashes: true}, "a\u00A0-\u202Fb-c-d"},
		{"homoglyph option keeps dashes", "—⸺⸻", CleanOptions{AggressiveHomoglyphs: true}, "—⸺⸻"},
		{"strip bidi", "\u061CA\u200E\u200F\u202BB\u202C\u2066C\u2069", CleanOptions{StripBidi: true}, "ABC"},
		{"strip emoji", "❤\uFE0F\u200D🔥", CleanOptions{StripEmojiGlue: true}, "❤🔥"},
		{"strip script glue", "می\u200Cروم ک\u0600", CleanOptions{StripEmojiGlue: true}, "میروم ک"},
		{"strip flag tags", "\U0001F3F4\U000E0067\U000E0062\U000E007F", CleanOptions{StripEmojiGlue: true}, "\U0001F3F4"},
		{"strip same-script selector", "葛\U000E0100 ᠠ\u180B ក\u17B4", CleanOptions{StripEmojiGlue: true}, "葛 ᠠ ក"},
		{"strip layout", "\U00013079\U00013430\U000130A7", CleanOptions{StripEmojiGlue: true}, "\U00013079\U000130A7"},
		{"glue flag keeps bidi", "a\u2066b\u2069", CleanOptions{StripEmojiGlue: true}, "a\u2066b\u2069"},
		{"map homoglyphs", "pаy ＡＢＣ ａｂｃ", CleanOptions{AggressiveHomoglyphs: true}, "pay ABC abc"},
		{"preserve nonmapped Cyrillic", "ЯДЖ", CleanOptions{AggressiveHomoglyphs: true}, "ЯДЖ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := CleanText(tt.input, tt.opts)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if twice, _ := CleanText(got, tt.opts); twice != got {
				t.Errorf("second pass changed %q to %q", got, twice)
			}
		})
	}
}

func TestCleanUnicodeAllNoncharacters(t *testing.T) {
	// Independent specification of all 66 permanently reserved noncharacters.
	var chars []rune
	for r := rune(0xFDD0); r <= 0xFDEF; r++ {
		chars = append(chars, r)
	}
	for plane := rune(0); plane <= 16; plane++ {
		chars = append(chars, plane<<16|0xFFFE, plane<<16|0xFFFF)
	}
	for _, ch := range chars {
		t.Run(fmt.Sprintf("U+%04X", ch), func(t *testing.T) {
			got, stats := CleanText("x"+string(ch)+"y", CleanOptions{NormalizeSpaces: true})
			if got != "xy" || stats.Removed != 1 {
				t.Errorf("got %q (%+v)", got, stats)
			}
		})
	}
	for _, ch := range []rune{0xFDF0, 0xFFFD} {
		text := "x" + string(ch) + "y"
		if got, _ := CleanText(text, CleanOptions{}); got != text {
			t.Errorf("assigned neighbour U+%04X changed", ch)
		}
	}
}

func TestCleanUnicodeExoticSpacesDoNotCollapse(t *testing.T) {
	// NBSP, Ogham, en/em quads/spaces, figures/punctuation/thin/hair spaces,
	// narrow NBSP, medium mathematical space, and ideographic space.
	spaces := "\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u202F\u205F\u3000"
	got, stats := CleanText("\t"+spaces+"\r\n", CleanOptions{NormalizeSpaces: true})
	want := "\t" + strings.Repeat(" ", utf8.RuneCountInString(spaces)) + "\r\n"
	if got != want || stats.Replaced != 16 || stats.Removed != 0 {
		t.Fatalf("got %q (%+v), want %q", got, stats, want)
	}
}

func TestCleanUnicodeAllReservedRanges(t *testing.T) {
	// Do not infer these from category Cn: newly assigned visible characters
	// must remain intact when Go updates its Unicode data.
	candidates := []rune{0x2065, 0xE0000}
	for _, interval := range [][2]rune{{0xFFF0, 0xFFF8}, {0xE0080, 0xE00FF}, {0xE01F0, 0xE0FFF}} {
		for ch := interval[0]; ch <= interval[1]; ch++ {
			candidates = append(candidates, ch)
		}
	}
	got, stats := CleanText("a"+string(candidates)+"b", CleanOptions{})
	if got != "ab" || stats.Removed != len(candidates) {
		t.Fatalf("reserved range not fully removed: length %d, stats %+v", len(got), stats)
	}
}

func FuzzCleanUnicodeIdempotence(f *testing.F) {
	for _, seed := range []string{
		"a\u180B\u180Cb", "a\u180E\u180Bb", "ᠠ\u180B\u180Cᠡ",
		"❤\uFE0F\u200D🔥", "می\u200Cروم", "葛\U000E0100\U000E0101",
		"a\u202Bb\u202Ec\u202Cd\u202Ce", "«\u00A0Texte\u202F!\u00A0»\r\n",
		"\U0001F3F4\U000E0067\U000E0062\U000E007F", "\U00013079\U00013430\U000130A7",
		"a—b⸺c⸻d–e−f\r\n", "—\u200B⸺\u00A0⸻\uFE0F",
	} {
		for optionBits := byte(0); optionBits < 32; optionBits++ {
			f.Add(seed, optionBits)
		}
	}
	f.Fuzz(func(t *testing.T, text string, optionBits byte) {
		if !utf8.ValidString(text) {
			t.Skip("file decoding rejects malformed UTF-8 before the Unicode pass")
		}
		opts := CleanOptions{
			NormalizeSpaces:      optionBits&1 != 0,
			NormalizeDashes:      optionBits&16 != 0,
			StripEmojiGlue:       optionBits&2 != 0,
			StripBidi:            optionBits&4 != 0,
			AggressiveHomoglyphs: optionBits&8 != 0,
		}
		cleaned, stats := CleanText(text, opts)
		if !utf8.ValidString(cleaned) || stats.InputRunes-stats.Removed != stats.OutputRunes {
			t.Fatalf("invalid output/counts: %q (%+v)", cleaned, stats)
		}
		if twice, again := CleanText(cleaned, opts); twice != cleaned || again.Removed != 0 || again.Replaced != 0 {
			t.Fatalf("not idempotent with %+v: %q -> %q -> %q", opts, text, cleaned, twice)
		}
	})
}
