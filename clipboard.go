package main

import (
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// A snapshot identifies what was checked. Windows also checks the sequence
// number under the clipboard lock before replacing it. Linux re-reads the
// same backend/target immediately before handing ownership to the copy tool.
type clipboardSnapshot struct {
	text     string
	sequence uint32
	backend  string
	target   string
}

var (
	errClipboardNoText  = errors.New("le presse-papiers ne contient pas de texte pris en charge")
	errClipboardChanged = errors.New("le presse-papiers a changé depuis l'analyse ; nouvelle copie conservée")
)

// Keep the UI/OS boundary injectable so the CLI and the decision to write are
// tested without reading or changing a user's desktop clipboard.
type clipboardOps struct {
	read         func(int64) (clipboardSnapshot, error)
	replace      func(clipboardSnapshot, string, int64) error
	startMonitor func(config) error
	monitor      func(config) error
	stopMonitor  func() error
}

func systemClipboardOps() clipboardOps {
	return clipboardOps{
		read: readClipboard, replace: replaceClipboard,
		startMonitor: startClipboardMonitor, monitor: runClipboardMonitor,
		stopMonitor: stopClipboardMonitor,
	}
}

// Clipboard text is already Unicode, not an encoded file: a leading U+FEFF
// is a text character here, not an encoding marker to preserve. The cleaner
// and options are otherwise exactly those used by -check and the file CLI.
func cleanClipboardText(cfg config, text string) (string, CleanStats, error) {
	if int64(len(text)) > cfg.maxBytes {
		return "", CleanStats{}, fmt.Errorf("presse-papiers supérieur à la limite de %d octets (-max-size)", cfg.maxBytes)
	}
	if !utf8.ValidString(text) {
		return "", CleanStats{}, errors.New("texte du presse-papiers non UTF-8 valide")
	}
	if err := validateText(text); err != nil {
		return "", CleanStats{}, err
	}
	if cfg.format == "json" {
		cleaned, stats, err := CleanJSON(text, cfg.CleanOptions)
		if err != nil {
			// JSON errors can contain object keys. Clipboard diagnostics must
			// not repeat any copied content in a terminal or native popup.
			return "", CleanStats{}, errors.New("JSON du presse-papiers invalide, chaînes Unicode invalides ou clés dupliquées après nettoyage ; contenu conservé")
		}
		return cleaned, stats, nil
	}
	cleaned, stats := CleanText(text, cfg.CleanOptions)
	return cleaned, stats, nil
}

func (a *application) processClipboard() {
	snapshot, err := a.clipboard.read(a.cfg.maxBytes)
	if errors.Is(err, errClipboardNoText) {
		a.report.Skipped++
		a.note("[ignoré] presse-papiers sans texte")
		return
	}
	if err != nil {
		a.error("presse-papiers", err)
		return
	}
	cleaned, stats, err := cleanClipboardText(a.cfg, snapshot.text)
	if err != nil {
		a.error("presse-papiers", err)
		return
	}
	changed := cleaned != snapshot.text
	a.collect(changed, stats)
	if !changed {
		return
	}
	if a.cfg.check || a.cfg.dryRun {
		a.note("[à nettoyer] presse-papiers (retirés=%d, remplacés=%d)", stats.Removed, stats.Replaced)
		return
	}
	if err := a.clipboard.replace(snapshot, cleaned, a.cfg.maxBytes); err != nil {
		a.error("presse-papiers", err)
		return
	}
	a.report.Written++
	a.note("[nettoyé] presse-papiers en texte brut (retirés=%d, remplacés=%d)", stats.Removed, stats.Replaced)
}

func runBackgroundCommand(cfg config, clipboard clipboardOps, stderr io.Writer) int {
	var err error
	switch {
	case cfg.backgroundStop:
		err = clipboard.stopMonitor()
	case cfg.backgroundWorker:
		err = clipboard.monitor(cfg)
	default:
		err = clipboard.startMonitor(cfg)
	}
	if err != nil {
		fmt.Fprintf(stderr, "rmwm: %v\n", err)
		return 2
	}
	if !cfg.quiet && !cfg.backgroundWorker {
		if cfg.backgroundStop {
			fmt.Fprintln(stderr, "rmwm: surveillance du presse-papiers arrêtée (ou déjà absente).")
		} else {
			fmt.Fprintln(stderr, "rmwm: surveillance du presse-papiers active ; arrêt : rmwm -bg-stop")
		}
	}
	return 0
}
