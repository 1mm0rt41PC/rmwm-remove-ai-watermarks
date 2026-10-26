//go:build !windows

package main

import "os"

// The temporary file is on the same filesystem as its destination.
func commitReplacement(temporary, destination string) (string, error) {
	return "", os.Rename(temporary, destination)
}
