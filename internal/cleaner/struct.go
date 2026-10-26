package cleaner

// ************************************************************************************************
// Opt selects stream cleaning rules. Boolean fields are opt-in.
// Empty Format means auto, which treats streams as text.
type Opt struct {
	Format              string
	NormalizeSpaces     bool
	NormalizeDashes     bool
	StripBidi           bool
	StripEmojiGlue      bool
	NormalizeHomoglyphs bool
}
