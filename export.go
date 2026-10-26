// Package rmwm provides deterministic Unicode and JSON cleanup for text and streams.
package rmwm

import (
	"io"

	"github.com/1mm0rt41PC/rmwm-remove-ai-watermarks/internal/cleaner"
)

// ************************************************************************************************
// Opt selects stream cleaning rules. Boolean fields are opt-in.
// Empty Format means auto, which treats streams as text.
type Opt = cleaner.Opt

// ************************************************************************************************
// CleanOptions controls the deterministic, local Unicode cleaning pass.
// All boolean fields are opt-in; CLI defaults are independent.
type CleanOptions = cleaner.CleanOptions

// ************************************************************************************************
// CleanStats counts Unicode code points, not UTF-8 bytes.
type CleanStats = cleaner.CleanStats

// ************************************************************************************************
// Sentinel errors retain the implementation's identity for errors.Is matching.
var (
	// ErrNilArgument indicates a nil reader or writer, including typed nils.
	ErrNilArgument = cleaner.ErrNilArgument
	// ErrInvalidFormat indicates a format other than empty, auto, text or json.
	ErrInvalidFormat = cleaner.ErrInvalidFormat
)

// ************************************************************************************************
// RunDry returns changed physical lines as before + "\n---\n" + after.
// Cleaning is document-wide; CRs and empty lines retain their positions. JSON
// results contain serialized lines, not decoded string tokens. BOMs are omitted.
func RunDry(reader io.Reader, opt Opt) ([]string, error) {
	return cleaner.RunDry(reader, opt)
}

// ************************************************************************************************
// HasWatermarks reports whether the selected deterministic rules change input.
// It validates the entire document and does not detect statistical watermarks.
func HasWatermarks(reader io.Reader, opt Opt) (bool, error) {
	return cleaner.HasWatermarks(reader, opt)
}

// ************************************************************************************************
// CleanupWatermarks validates and cleans all input before writing any output.
// Encoding and BOM are preserved. Stream size is unlimited; writer failures and
// short writes are returned, but an already partially written output cannot roll back.
func CleanupWatermarks(reader io.Reader, writer io.Writer, opt Opt) error {
	return cleaner.CleanupWatermarks(reader, writer, opt)
}

// ************************************************************************************************
// CleanText removes deterministic Unicode carriers and applies the selected
// space and dash normalization. It does not detect/remove statistical text
// watermarks. Input must be valid UTF-8; encoding validation and file format
// detection belong to the I/O layer. Whitespace runs, indentation, line endings,
// and final newlines are preserved.
func CleanText(text string, opts CleanOptions) (string, CleanStats) {
	return cleaner.CleanText(text, opts)
}

// ************************************************************************************************
// CleanJSON cleans decoded JSON strings, including object keys and Unicode
// escapes. Bytes outside changed string tokens are kept exactly as supplied.
// Changed strings are re-encoded as JSON, so their original escape spelling can
// change. InputRunes and OutputRunes count the complete serialized documents;
// Removed and Replaced count characters in the decoded strings.
// Invalid UTF-8, invalid JSON, unpaired UTF-16 surrogate escapes, and duplicate
// object keys (including collisions after cleaning) return no partial result.
func CleanJSON(text string, opts CleanOptions) (string, CleanStats, error) {
	return cleaner.CleanJSON(text, opts)
}
