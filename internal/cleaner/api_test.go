package cleaner

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/1mm0rt41PC/rmwm-remove-ai-watermarks/internal/textio"
)

// ************************************************************************************************
// TestStreamAPI covers document context, serialized JSON and physical alignment.
func TestStreamAPI(t *testing.T) {
	tests := []struct {
		name, input, output string
		opt                 Opt
		dry                 []string
	}{
		{testId("empty"), "", "", Opt{}, nil},
		{testId("exposed text signature"), "\u200b%PDF-text", "%PDF-text", Opt{}, []string{"\u200b%PDF-text\n---\n%PDF-text"}},
		{testId("alignment"), "\na\u200b\r\n\u200b\nlast\n", "\na\r\n\nlast\n", Opt{}, []string{"a\u200b\r\n---\na\r", "\u200b\n---\n"}},
		{testId("no boolean defaults"), "a\u00a0—b", "a\u00a0—b", Opt{}, nil},
		{testId("normalization"), "a\u00a0—b", "a -b", Opt{NormalizeSpaces: true, NormalizeDashes: true}, []string{"a\u00a0—b\n---\na -b"}},
		{testId("document bidi"), "\u202aone\ntwo\u202c", "\u202aone\ntwo\u202c", Opt{}, nil},
		{testId("bidi opt-in"), "\u202aone\ntwo\u202c", "one\ntwo", Opt{StripBidi: true}, []string{"\u202aone\n---\none", "two\u202c\n---\ntwo"}},
		{testId("JSON physical lines"), "{\r\n  \"a\": \"x\\n\\u200by\"\r\n}\n", "{\r\n  \"a\": \"x\\ny\"\r\n}\n", Opt{Format: "json"}, []string{"  \"a\": \"x\\n\\u200by\"\r\n---\n  \"a\": \"x\\ny\"\r"}},
		{testId("auto stream is text"), `"x\u200b"`, `"x\u200b"`, Opt{}, nil},
		{testId("homoglyph"), "Ａа", "Aa", Opt{NormalizeHomoglyphs: true}, []string{"Ａа\n---\nAa"}},
		{testId("emoji glue"), "👩‍💻", "👩💻", Opt{StripEmojiGlue: true}, []string{"👩‍💻\n---\n👩💻"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := CleanupWatermarks(strings.NewReader(tt.input), &output, tt.opt); err != nil || output.String() != tt.output {
				t.Errorf("cleanup = %q, %v; want %q", output.String(), err, tt.output)
			}
			has, err := HasWatermarks(strings.NewReader(tt.input), tt.opt)
			if err != nil || has != (tt.input != tt.output) {
				t.Errorf("has = %v, %v", has, err)
			}
			dry, err := RunDry(strings.NewReader(tt.input), tt.opt)
			if err != nil || !reflect.DeepEqual(dry, tt.dry) {
				t.Errorf("dry = %#v, %v; want %#v", dry, err, tt.dry)
			}
		})
	}
}

// ************************************************************************************************
// TestStreamEncoding ensures all supported encodings preserve their BOMs.
func TestStreamEncoding(t *testing.T) {
	for _, encoding := range []textio.Encoding{textio.UTF8, textio.UTF8BOM, textio.UTF16LE, textio.UTF16BE} {
		input := textio.EncodeText("a\u200b\r\n", encoding)
		var output bytes.Buffer
		if err := CleanupWatermarks(bytes.NewReader(input), &output, Opt{}); err != nil || !bytes.Equal(output.Bytes(), textio.EncodeText("a\r\n", encoding)) {
			t.Errorf("encoding %v: %x, %v", encoding, output.Bytes(), err)
		}
		dry, err := RunDry(bytes.NewReader(input), Opt{})
		if err != nil || !reflect.DeepEqual(dry, []string{"a\u200b\r\n---\na\r"}) {
			t.Errorf("dry encoding %v: %q, %v", encoding, dry, err)
		}
	}
}

// ************************************************************************************************
// TestStreamValidation ensures failures do not write partially validated data.
func TestStreamValidation(t *testing.T) {
	tests := []struct {
		name, input string
		opt         Opt
	}{
		{testId("format"), "hello", Opt{Format: "xml"}},
		{testId("binary"), "a\x00", Opt{}},
		{testId("invalid utf8"), "\xff", Opt{}},
		{testId("JSON syntax"), "{", Opt{Format: "json"}},
		{testId("JSON collision"), `{"a":1,"a\u200b":2}`, Opt{Format: "json"}},
		{testId("JSON surrogate"), `"\ud800"`, Opt{Format: "json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := CleanupWatermarks(strings.NewReader(tt.input), &output, tt.opt); err == nil || output.Len() != 0 {
				t.Errorf("cleanup wrote %q, err=%v", output.String(), err)
			}
			if has, err := HasWatermarks(strings.NewReader(tt.input), tt.opt); err == nil || has {
				t.Errorf("has=%v err=%v", has, err)
			}
			if dry, err := RunDry(strings.NewReader(tt.input), tt.opt); err == nil || dry != nil {
				t.Errorf("dry=%q err=%v", dry, err)
			}
		})
	}
}

// ************************************************************************************************
// apiFailureWriter injects writer errors and short writes.
type apiFailureWriter struct{ err error }

// ************************************************************************************************
// Write deliberately writes no bytes.
func (w apiFailureWriter) Write(p []byte) (int, error) { return 0, w.err }

// ************************************************************************************************
// TestStreamIOErrors exercises nil arguments, read failures and write failures.
func TestStreamIOErrors(t *testing.T) {
	var nilBuffer *bytes.Buffer
	for _, reader := range []io.Reader{nil, nilBuffer} {
		if _, err := HasWatermarks(reader, Opt{}); !errors.Is(err, ErrNilArgument) {
			t.Errorf("nil reader: %v", err)
		}
	}
	for _, writer := range []io.Writer{nil, nilBuffer} {
		if err := CleanupWatermarks(strings.NewReader("ok"), writer, Opt{}); !errors.Is(err, ErrNilArgument) {
			t.Errorf("nil writer: %v", err)
		}
	}
	if err := CleanupWatermarks(strings.NewReader("ok"), apiFailureWriter{}, Opt{}); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("short write: %v", err)
	}
	want := errors.New("injected")
	if err := CleanupWatermarks(strings.NewReader("ok"), apiFailureWriter{want}, Opt{}); !errors.Is(err, want) {
		t.Errorf("writer: %v", err)
	}
	original := mock_ioReadAll
	t.Cleanup(func() { mock_ioReadAll = original })
	mock_ioReadAll = func(io.Reader) ([]byte, error) { return []byte("partial"), want }
	if found, err := HasWatermarks(strings.NewReader("ok"), Opt{}); found || !errors.Is(err, want) {
		t.Errorf("detection read error: %v, %v", found, err)
	}
	if parts, err := RunDry(strings.NewReader("ok"), Opt{}); parts != nil || !errors.Is(err, want) {
		t.Errorf("preview read error: %v, %v", parts, err)
	}
	var output bytes.Buffer
	if err := CleanupWatermarks(strings.NewReader("ok"), &output, Opt{}); !errors.Is(err, want) || output.Len() != 0 {
		t.Errorf("reader: %v, output=%q", err, output.String())
	}
}

// ************************************************************************************************
// TestStreamLargeInput verifies whole-document processing beyond scanner limits.
func TestStreamLargeInput(t *testing.T) {
	input := strings.Repeat("a", 2*1024*1024)
	var output bytes.Buffer
	if err := CleanupWatermarks(strings.NewReader(input+"\u200b"), &output, Opt{}); err != nil || output.String() != input {
		t.Errorf("large stream: len=%d, err=%v", output.Len(), err)
	}
}
