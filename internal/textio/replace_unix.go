//go:build !windows

package textio

import "os"

// The temporary file is on the same filesystem as its destination.
func commitReplacement(temporary, destination string) (string, error) {
	return "", os.Rename(temporary, destination)
}
