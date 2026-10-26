//go:build windows

package main

import (
	"errors"
	"testing"
)

func TestWindowsClipboardEarlyCancellation(t *testing.T) {
	// Cancellation must return before window creation, allocation, or any real
	// clipboard read/write. This test never accesses the desktop clipboard.
	previous := clipboardSnapshot{text: "private\u200B", sequence: 1, backend: "win32", target: "CF_UNICODETEXT"}
	err := replaceWindowsClipboardWithGuard(previous, "private", 1024, func() bool { return true })
	if !errors.Is(err, errClipboardChanged) {
		t.Fatalf("cancelled write = %v", err)
	}
}
