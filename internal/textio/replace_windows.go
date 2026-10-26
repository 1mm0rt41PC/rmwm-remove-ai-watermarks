//go:build windows

package textio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var replaceFileW = syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW")
var moveFileW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileW")

func widePath(path string) (*uint16, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(abs, `\\?\`) {
		if strings.HasPrefix(abs, `\\`) {
			abs = `\\?\UNC\` + strings.TrimPrefix(abs, `\\`)
		} else {
			abs = `\\?\` + abs
		}
	}
	return syscall.UTF16PtrFromString(abs)
}

// ReplaceFileW preserves the destination's DACL, creation time and named
// streams. Do not use flags that silently ignore ACL merge errors.
// Always ask Windows for a temporary rollback copy, including -no-backup:
// certain documented errors can otherwise remove the original name.
func commitReplacement(temporary, destination string) (string, error) {
	originalInfo, err := os.Lstat(destination)
	if err != nil {
		return "", err
	}
	// SameFile also forces Go's Windows FileInfo to cache its lazily loaded
	// file ID now, while the original still has this name.
	if !originalInfo.Mode().IsRegular() || !os.SameFile(originalInfo, originalInfo) {
		return "", fmt.Errorf("destination non régulière ou identité inaccessible")
	}
	rollbackFile, err := os.CreateTemp(filepath.Dir(destination), ".rmwm-rollback-*.tmp")
	if err != nil {
		return "", err
	}
	rollback := rollbackFile.Name()
	placeholderInfo, statErr := rollbackFile.Stat()
	if err := rollbackFile.Close(); err != nil {
		_ = os.Remove(rollback)
		return "", err
	}
	if statErr != nil {
		_ = os.Remove(rollback)
		return "", statErr
	}
	// The API requires a backup name. This unique same-directory name belongs
	// to us; ReplaceFileW replaces the empty placeholder with the original.
	dest, err := widePath(destination)
	if err != nil {
		_ = os.Remove(rollback)
		return "", err
	}
	temp, err := widePath(temporary)
	if err != nil {
		_ = os.Remove(rollback)
		return "", err
	}
	bak, err := widePath(rollback)
	if err != nil {
		_ = os.Remove(rollback)
		return "", err
	}
	ok, _, callErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(dest)), uintptr(unsafe.Pointer(temp)),
		uintptr(unsafe.Pointer(bak)), 0, 0, 0,
	)
	if ok != 0 {
		if err := os.Remove(rollback); err != nil {
			return fmt.Sprintf("fichier remplacé ; sauvegarde temporaire non supprimée %s : %v", rollback, err), nil
		}
		return "", nil
	}
	rollbackInfo, rollbackErr := os.Lstat(rollback)
	isOriginalRollback := rollbackErr == nil && rollbackInfo.Mode().IsRegular() && os.SameFile(originalInfo, rollbackInfo)
	isPlaceholder := rollbackErr == nil && os.SameFile(placeholderInfo, rollbackInfo)
	// If Windows moved the original before failing, restore its name without
	// overwriting anything. In all other cases retain a real rollback file.
	if _, statErr := os.Lstat(destination); os.IsNotExist(statErr) {
		if isOriginalRollback {
			if e := moveWithoutReplace(rollback, destination); e == nil {
				return "", fmt.Errorf("ReplaceFileW : %w (original restauré)", callErr)
			}
			return "", fmt.Errorf("ReplaceFileW : %w ; original récupérable dans %s", callErr, rollback)
		}
		return "", fmt.Errorf("ReplaceFileW : %w ; destination absente, aucune restauration automatique possible", callErr)
	}
	// Remove only the exact empty placeholder we created, identified by file
	// ID. Never mistake a real rollback (even an empty original) for it.
	if isPlaceholder {
		_ = os.Remove(rollback)
		return "", fmt.Errorf("ReplaceFileW : %w", callErr)
	}
	if isOriginalRollback {
		return "", fmt.Errorf("ReplaceFileW : %w ; sauvegarde temporaire conservée dans %s", callErr, rollback)
	}
	return "", fmt.Errorf("ReplaceFileW : %w", callErr)
}

// Unlike os.Rename on Windows, MoveFileW fails if the destination already
// exists, including if another process recreates it during recovery.
func moveWithoutReplace(source, destination string) error {
	src, err := widePath(source)
	if err != nil {
		return err
	}
	dst, err := widePath(destination)
	if err != nil {
		return err
	}
	ok, _, callErr := moveFileW.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(dst)))
	if ok == 0 {
		return callErr
	}
	return nil
}
