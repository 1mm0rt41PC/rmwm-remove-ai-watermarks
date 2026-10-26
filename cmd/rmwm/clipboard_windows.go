//go:build windows

package main

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

var (
	clipboardUser32      = syscall.NewLazyDLL("user32.dll")
	clipboardKernel32    = syscall.NewLazyDLL("kernel32.dll")
	clipboardNTDLL       = syscall.NewLazyDLL("ntdll.dll")
	winOpenClipboard     = clipboardUser32.NewProc("OpenClipboard")
	winCloseClipboard    = clipboardUser32.NewProc("CloseClipboard")
	winIsClipboardFormat = clipboardUser32.NewProc("IsClipboardFormatAvailable")
	winGetClipboardData  = clipboardUser32.NewProc("GetClipboardData")
	winSetClipboardData  = clipboardUser32.NewProc("SetClipboardData")
	winEmptyClipboard    = clipboardUser32.NewProc("EmptyClipboard")
	winClipboardSequence = clipboardUser32.NewProc("GetClipboardSequenceNumber")
	winCreateWindowEx    = clipboardUser32.NewProc("CreateWindowExW")
	winDestroyWindow     = clipboardUser32.NewProc("DestroyWindow")
	winGlobalAlloc       = clipboardKernel32.NewProc("GlobalAlloc")
	winGlobalFree        = clipboardKernel32.NewProc("GlobalFree")
	winGlobalLock        = clipboardKernel32.NewProc("GlobalLock")
	winGlobalUnlock      = clipboardKernel32.NewProc("GlobalUnlock")
	winGlobalSize        = clipboardKernel32.NewProc("GlobalSize")
	winMoveMemory        = clipboardNTDLL.NewProc("RtlMoveMemory")
)

const (
	windowsUnicodeText = 13
	windowsFileDrop    = 15
	windowsMoveable    = 0x0002
)

func windowsCallError(operation string, err error) error {
	if err == nil || err == syscall.Errno(0) {
		return fmt.Errorf("%s a échoué", operation)
	}
	return fmt.Errorf("%s : %w", operation, err)
}

// OpenClipboard/CloseClipboard are kept on one OS thread. A short retry handles
// normal contention without retaining a lock while the user is deciding.
func openWindowsClipboard(owner uintptr) error {
	deadline := time.Now().Add(800 * time.Millisecond)
	for {
		ok, _, err := winOpenClipboard.Call(owner)
		if ok != 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			return windowsCallError("OpenClipboard (presse-papiers occupé)", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readClipboard(maxBytes int64) (clipboardSnapshot, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openWindowsClipboard(0); err != nil {
		return clipboardSnapshot{}, err
	}
	defer winCloseClipboard.Call()
	return readWindowsClipboardLocked(maxBytes)
}

// Caller owns the OpenClipboard lock. Reading delayed-rendered data can change
// its sequence number, so snapshot it AFTER GetClipboardData and decoding.
func readWindowsClipboardLocked(maxBytes int64) (clipboardSnapshot, error) {
	if maxBytes <= 0 {
		return clipboardSnapshot{}, fmt.Errorf("limite du presse-papiers invalide")
	}
	files, _, _ := winIsClipboardFormat.Call(windowsFileDrop)
	if files != 0 {
		// Explorer can publish text alongside CF_HDROP. A file-copy operation
		// must never be replaced with a cleaned list of path names.
		return clipboardSnapshot{}, errClipboardNoText
	}
	available, _, _ := winIsClipboardFormat.Call(windowsUnicodeText)
	if available == 0 {
		return clipboardSnapshot{}, errClipboardNoText
	}
	handle, _, err := winGetClipboardData.Call(windowsUnicodeText)
	if handle == 0 {
		return clipboardSnapshot{}, windowsCallError("GetClipboardData", err)
	}
	size, _, err := winGlobalSize.Call(handle)
	if size < 2 {
		return clipboardSnapshot{}, fmt.Errorf("presse-papiers UTF-16 invalide (taille mémoire)")
	}
	// GlobalSize is allocation capacity and may exceed the text size. Read at
	// most maxBytes UTF-16 units plus NUL, then validate the decoded UTF-8 size.
	// Never allocate an unbounded Go slice from clipboard-provided capacity.
	maxInt := uint64(^uint(0) >> 1)
	units := uint64(size / 2)
	if units > uint64(maxBytes)+1 {
		units = uint64(maxBytes) + 1
	}
	if units > maxInt/2 {
		return clipboardSnapshot{}, fmt.Errorf("texte du presse-papiers supérieur à la limite autorisée")
	}
	ptr, _, err := winGlobalLock.Call(handle)
	if ptr == 0 {
		return clipboardSnapshot{}, windowsCallError("GlobalLock (lecture)", err)
	}
	defer winGlobalUnlock.Call(handle)
	words := make([]uint16, int(units))
	// Copy foreign memory into a bounded Go-owned buffer instead of converting
	// a Win32 uintptr into a Go pointer. The source stays GlobalLocked throughout.
	winMoveMemory.Call(uintptr(unsafe.Pointer(&words[0])), ptr, uintptr(units*2))
	text, err := decodeWindowsClipboardText(words, maxBytes)
	if err != nil {
		return clipboardSnapshot{}, err
	}
	sequence, _, _ := winClipboardSequence.Call()
	if sequence == 0 {
		return clipboardSnapshot{}, fmt.Errorf("numéro de séquence du presse-papiers inaccessible")
	}
	return clipboardSnapshot{
		text: text, sequence: uint32(sequence), backend: "win32", target: "CF_UNICODETEXT",
	}, nil
}

// A non-NULL HWND is required: EmptyClipboard would otherwise leave no owner
// and SetClipboardData could fail. STATIC is a built-in window class; HWND_MESSAGE
// creates an invisible message-only window, destroyed on this same OS thread.
func newClipboardOwner() (uintptr, error) {
	class, _ := syscall.UTF16PtrFromString("STATIC")
	name, _ := syscall.UTF16PtrFromString("rmwm clipboard owner")
	hwnd, _, err := winCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(name)), 0, 0, 0, 0, 0, ^uintptr(2), 0, 0, 0)
	if hwnd == 0 {
		return 0, windowsCallError("CreateWindowExW (propriétaire du presse-papiers)", err)
	}
	return hwnd, nil
}

func allocateWindowsClipboardText(text string, maxBytes int64) (uintptr, error) {
	if maxBytes <= 0 || int64(len(text)) > maxBytes {
		return 0, fmt.Errorf("texte du presse-papiers supérieur à la limite autorisée")
	}
	if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return 0, fmt.Errorf("texte non représentable en CF_UNICODETEXT (UTF-8 invalide ou NUL)")
	}
	units := uint64(1) // NUL terminator, including for empty text.
	for _, ch := range text {
		units++
		if ch > 0xFFFF {
			units++
		}
	}
	if units > uint64(^uint(0)>>1)/2 {
		return 0, fmt.Errorf("texte du presse-papiers trop grand pour cette architecture")
	}
	handle, _, err := winGlobalAlloc.Call(windowsMoveable, uintptr(units*2))
	if handle == 0 {
		return 0, windowsCallError("GlobalAlloc", err)
	}
	ptr, _, err := winGlobalLock.Call(handle)
	if ptr == 0 {
		winGlobalFree.Call(handle)
		return 0, windowsCallError("GlobalLock (écriture)", err)
	}
	words := make([]uint16, int(units))
	i := 0
	for _, ch := range text {
		if ch > 0xFFFF {
			hi, lo := utf16.EncodeRune(ch)
			words[i], words[i+1] = uint16(hi), uint16(lo)
			i += 2
		} else {
			words[i] = uint16(ch)
			i++
		}
	}
	words[i] = 0
	winMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&words[0])), uintptr(units*2))
	winGlobalUnlock.Call(handle)
	return handle, nil
}

func replaceClipboard(previous clipboardSnapshot, text string, maxBytes int64) error {
	return replaceWindowsClipboardWithGuard(previous, text, maxBytes, nil)
}

// A monitor can be stopped while allocation or OpenClipboard is pending. Its
// cancellation guard is checked again under the clipboard lock immediately
// before the destructive call. Once EmptyClipboard starts, finish the atomic
// replacement/restoration; shutdown waits for that operation to complete.
func replaceWindowsClipboardWithGuard(previous clipboardSnapshot, text string, maxBytes int64, shouldAbort func() bool) error {
	if previous.backend != "win32" || previous.target != "CF_UNICODETEXT" || previous.sequence == 0 {
		return errClipboardChanged
	}
	if shouldAbort != nil && shouldAbort() {
		return errClipboardChanged
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	owner, err := newClipboardOwner()
	if err != nil {
		return err
	}
	defer winDestroyWindow.Call(owner)

	// Allocate BOTH values before the destructive call. A successful
	// SetClipboardData transfers ownership; only untransferred handles are freed.
	replacement, err := allocateWindowsClipboardText(text, maxBytes)
	if err != nil {
		return err
	}
	defer func() {
		if replacement != 0 {
			winGlobalFree.Call(replacement)
		}
	}()
	recovery, err := allocateWindowsClipboardText(previous.text, maxBytes)
	if err != nil {
		return err
	}
	defer func() {
		if recovery != 0 {
			winGlobalFree.Call(recovery)
		}
	}()
	if err := openWindowsClipboard(owner); err != nil {
		return err
	}
	defer winCloseClipboard.Call()

	// Both sequence AND contents are checked while holding the SAME lock as
	// EmptyClipboard/SetClipboardData. A newer copy during a popup is never lost.
	sequence, _, _ := winClipboardSequence.Call()
	if uint32(sequence) != previous.sequence {
		return errClipboardChanged
	}
	current, err := readWindowsClipboardLocked(maxBytes)
	if err != nil || current.sequence != previous.sequence || current.text != previous.text {
		return errClipboardChanged
	}
	if current.text == text {
		return nil
	}
	if shouldAbort != nil && shouldAbort() {
		return errClipboardChanged
	}
	ok, _, err := winEmptyClipboard.Call()
	if ok == 0 {
		return windowsCallError("EmptyClipboard", err)
	}
	ok, _, setErr := winSetClipboardData.Call(windowsUnicodeText, replacement)
	if ok != 0 {
		replacement = 0 // Windows now owns this allocation.
		return nil
	}
	ok, _, restoreErr := winSetClipboardData.Call(windowsUnicodeText, recovery)
	if ok != 0 {
		recovery = 0
		return fmt.Errorf("%w ; texte original restauré", windowsCallError("SetClipboardData", setErr))
	}
	return fmt.Errorf("%w ; restauration impossible (%v), le texte doit être recopié",
		windowsCallError("SetClipboardData", setErr), windowsCallError("SetClipboardData (restauration)", restoreErr))
}
