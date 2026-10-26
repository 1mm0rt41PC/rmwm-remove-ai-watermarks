package cleaner

import (
	"bytes"
	"io"
	"reflect"
	"strings"
)

// ************************************************************************************************
// RunDry returns changed physical lines as before + "\n---\n" + after.
// Cleaning is document-wide; CRs and empty lines retain their positions. JSON
// results contain serialized lines, not decoded string tokens. BOMs are omitted.
func RunDry(reader io.Reader, opt Opt) ([]string, error) {
	before, after, err := mk_cleanStream(reader, opt)
	if err != nil {
		return nil, err
	}
	oldText, _, err := mk_decodeText(before)
	if err != nil {
		return nil, err
	}
	newText, _, err := mk_decodeTextBytes(after)
	if err != nil {
		return nil, err
	}
	oldLines, newLines := strings.Split(oldText, "\n"), strings.Split(newText, "\n")
	var result []string
	for i, line := range oldLines {
		if line != newLines[i] {
			result = append(result, line+"\n---\n"+newLines[i])
		}
	}
	return result, nil
}

// ************************************************************************************************
// HasWatermarks reports whether the selected deterministic rules change input.
// It validates the entire document and does not detect statistical watermarks.
func HasWatermarks(reader io.Reader, opt Opt) (bool, error) {
	before, after, err := mk_cleanStream(reader, opt)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(before, after), err
}

// ************************************************************************************************
// CleanupWatermarks validates and cleans all input before writing any output.
// Encoding and BOM are preserved. Stream size is unlimited; writer failures and
// short writes are returned, but an already partially written output cannot roll back.
func CleanupWatermarks(reader io.Reader, writer io.Writer, opt Opt) error {
	if mk_nilInterface(writer) {
		return ErrNilArgument
	}
	_, after, err := mk_cleanStream(reader, opt)
	if err != nil {
		return err
	}
	return mk_writeAll(writer, after)
}

// ************************************************************************************************
// cleanStream validates applicable options and reads the complete stream.
func cleanStream(reader io.Reader, opt Opt) ([]byte, []byte, error) {
	if mk_nilInterface(reader) {
		return nil, nil, ErrNilArgument
	}
	cfg, err := mk_streamConfig(opt)
	if err != nil {
		return nil, nil, err
	}
	data, err := mock_ioReadAll(reader)
	if err != nil {
		return nil, nil, err
	}
	cleaned, _, err := mk_cleanDocument(data, "", cfg)
	return data, cleaned, err
}

// ************************************************************************************************
// streamConfig maps explicit options without enabling any boolean defaults.
func streamConfig(opt Opt) (cleanSettings, error) {
	format := opt.Format
	if format == "" {
		format = "auto"
	}
	if format != "auto" && format != "text" && format != "json" {
		return cleanSettings{}, ErrInvalidFormat
	}
	return cleanSettings{format: format, CleanOptions: CleanOptions{
		NormalizeSpaces: opt.NormalizeSpaces, NormalizeDashes: opt.NormalizeDashes,
		StripBidi: opt.StripBidi, StripEmojiGlue: opt.StripEmojiGlue,
		AggressiveHomoglyphs: opt.NormalizeHomoglyphs,
	}}, nil
}

// ************************************************************************************************
// nilInterface also recognizes typed nil readers and writers.
func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}
