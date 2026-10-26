//go:build !windows

package textio

import "os"

// New files start at 0600 on Unix. The caller applies the final permission
// bits only after writing, so copying a Windows DACL has no counterpart here.
func protectNewFile(file *os.File, sourcePath string) error {
	return nil
}
