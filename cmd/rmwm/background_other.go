//go:build !windows

package main

import "errors"

func startClipboardMonitor(config) error {
	return errors.New("-bg est disponible uniquement sous Windows ; sous Linux utiliser rmwm '~'")
}

func runClipboardMonitor(config) error {
	return errors.New("la surveillance du presse-papiers est disponible uniquement sous Windows")
}

func stopClipboardMonitor() error {
	return errors.New("-bg-stop est disponible uniquement sous Windows")
}
