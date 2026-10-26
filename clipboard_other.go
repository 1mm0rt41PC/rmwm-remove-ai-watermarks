//go:build !windows && !linux

package main

import "errors"

func readClipboard(int64) (clipboardSnapshot, error) {
	return clipboardSnapshot{}, errors.New("le mode ~ est disponible sous Windows et Linux (Wayland/X11)")
}

func replaceClipboard(clipboardSnapshot, string, int64) error {
	return errors.New("le mode ~ est disponible sous Windows et Linux (Wayland/X11)")
}
