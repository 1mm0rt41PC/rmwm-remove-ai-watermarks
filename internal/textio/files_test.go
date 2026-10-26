package textio

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ************************************************************************************************
// TestDecodePolicySeams verifies that decoding and validation failures propagate.
func TestDecodePolicySeams(t *testing.T) {
	originalDecode, originalValidate := mk_decodeTextBytes, mk_validateText
	t.Cleanup(func() { mk_decodeTextBytes, mk_validateText = originalDecode, originalValidate })
	want := errors.New("injected decode failure")
	mk_decodeTextBytes = func([]byte) (string, Encoding, error) { return "", UTF16BE, want }
	if _, encoding, err := DecodeText(nil); encoding != UTF16BE || !errors.Is(err, want) {
		t.Errorf("decode: encoding=%v error=%v", encoding, err)
	}
	mk_decodeTextBytes = originalDecode
	mk_validateText = func(string) error { return want }
	if _, _, err := DecodeText([]byte("valid")); !errors.Is(err, want) {
		t.Errorf("validation: %v", err)
	}
}

// ************************************************************************************************
// TestReplacementRejectsChangedSource ensures extraction retains the snapshot guard.
func TestReplacementRejectsChangedSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Errorf("setup: %v", err)
		return
	}
	source, err := ReadSource(path, 1024)
	if err != nil {
		t.Errorf("snapshot: %v", err)
		return
	}
	if err := os.WriteFile(path, []byte("new user content"), 0600); err != nil {
		t.Errorf("mutation: %v", err)
		return
	}
	if _, _, err := ReplaceSource(source, []byte("cleaned"), true); err == nil {
		t.Errorf("accepted changed source")
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, []byte("new user content")) {
		t.Errorf("source changed: %q, %v", data, err)
	}
	if _, err := os.Lstat(path + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unexpected backup: %v", err)
	}
}
