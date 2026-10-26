//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	linuxClipboardTimeout       = 5 * time.Second
	linuxClipboardWaitDelay     = 200 * time.Millisecond
	linuxClipboardTypesLimit    = 64 << 10
	linuxClipboardDiagnosticMax = 4096
)

type linuxClipboardBackend struct {
	name   string
	reader string
	writer string
}

// Dependency injection keeps the backend tests away from the user's desktop.
// A nil input means a read; a non-nil input is sent verbatim to a writer's stdin.
type linuxClipboardClient struct {
	getenv   func(string) string
	lookPath func(string) (string, error)
	run      func(string, []string, *string, int64) ([]byte, error)
}

func systemLinuxClipboard() linuxClipboardClient {
	return linuxClipboardClient{
		getenv: os.Getenv, lookPath: exec.LookPath,
		run: func(program string, args []string, input *string, maximum int64) ([]byte, error) {
			return runLinuxClipboardCommand(program, args, input, maximum, linuxClipboardTimeout)
		},
	}
}

func readClipboard(maxBytes int64) (clipboardSnapshot, error) {
	return systemLinuxClipboard().read(maxBytes)
}

func replaceClipboard(previous clipboardSnapshot, text string, maxBytes int64) error {
	return systemLinuxClipboard().replace(previous, text, maxBytes)
}

func (client linuxClipboardClient) backend(name string) (linuxClipboardBackend, error) {
	if name == "" {
		switch {
		case client.getenv("WAYLAND_DISPLAY") != "" || strings.EqualFold(client.getenv("XDG_SESSION_TYPE"), "wayland"):
			name = "wayland"
		case client.getenv("DISPLAY") != "":
			name = "xclip"
		default:
			return linuxClipboardBackend{}, errors.New("presse-papiers Linux indisponible : aucune session Wayland/X11 (WAYLAND_DISPLAY ou DISPLAY)")
		}
	}

	backend := linuxClipboardBackend{name: name}
	var err error
	switch name {
	case "wayland":
		if client.getenv("WAYLAND_DISPLAY") == "" && !strings.EqualFold(client.getenv("XDG_SESSION_TYPE"), "wayland") {
			return backend, errors.New("la session Wayland du presse-papiers n'est plus disponible")
		}
		backend.reader, err = client.lookPath("wl-paste")
		if err == nil {
			backend.writer, err = client.lookPath("wl-copy")
		}
		if err != nil {
			return backend, fmt.Errorf("presse-papiers Wayland : installez wl-clipboard (wl-paste et wl-copy dans PATH) : %w", err)
		}
	case "xclip":
		if client.getenv("DISPLAY") == "" {
			return backend, errors.New("presse-papiers X11 indisponible : DISPLAY est absent")
		}
		backend.reader, err = client.lookPath("xclip")
		if err != nil {
			return backend, fmt.Errorf("presse-papiers X11 : installez xclip dans PATH : %w", err)
		}
		backend.writer = backend.reader
	default:
		return backend, fmt.Errorf("backend de presse-papiers Linux inconnu : %q", name)
	}
	return backend, nil
}

func (client linuxClipboardClient) types(backend linuxClipboardBackend) ([]string, error) {
	args := []string{"--list-types"}
	if backend.name == "xclip" {
		args = []string{"-selection", "clipboard", "-out", "-target", "TARGETS"}
	}
	data, err := client.run(backend.reader, args, nil, linuxClipboardTypesLimit)
	if err != nil {
		if linuxClipboardNoSelection(err, backend.name) {
			return nil, errClipboardNoText
		}
		return nil, fmt.Errorf("lecture des formats du presse-papiers %s : %w", backend.name, err)
	}
	var types []string
	for _, line := range strings.Split(string(data), "\n") {
		// This trims only the type-list metadata, never clipboard text.
		if target := strings.TrimSpace(line); target != "" {
			types = append(types, target)
		}
	}
	return types, nil
}

func (client linuxClipboardClient) readTarget(backend linuxClipboardBackend, target string, maxBytes int64) (string, error) {
	args := []string{"--no-newline", "--type", target}
	if backend.name == "xclip" {
		args = []string{"-selection", "clipboard", "-out", "-target", target}
	}
	data, err := client.run(backend.reader, args, nil, maxBytes)
	if err != nil {
		if linuxClipboardNoSelection(err, backend.name) {
			return "", errClipboardNoText
		}
		return "", fmt.Errorf("lecture du texte du presse-papiers %s : %w", backend.name, err)
	}
	if int64(len(data)) > maxBytes {
		return "", fmt.Errorf("texte du presse-papiers supérieur à la limite de %d octets (-max-size)", maxBytes)
	}
	if !utf8.Valid(data) {
		return "", errors.New("le texte du presse-papiers Linux n'est pas un UTF-8 valide")
	}
	return string(data), nil
}

func (client linuxClipboardClient) read(maxBytes int64) (clipboardSnapshot, error) {
	if maxBytes <= 0 {
		return clipboardSnapshot{}, errors.New("limite du presse-papiers invalide")
	}
	backend, err := client.backend("")
	if err != nil {
		return clipboardSnapshot{}, err
	}
	types, err := client.types(backend)
	if err != nil {
		return clipboardSnapshot{}, err
	}
	target, err := linuxClipboardTextTarget(types, backend.name)
	if err != nil {
		return clipboardSnapshot{}, err
	}
	text, err := client.readTarget(backend, target, maxBytes)
	if err != nil {
		return clipboardSnapshot{}, err
	}
	return clipboardSnapshot{text: text, backend: backend.name, target: target}, nil
}

func (client linuxClipboardClient) replace(previous clipboardSnapshot, text string, maxBytes int64) error {
	if maxBytes <= 0 || int64(len(text)) > maxBytes {
		return fmt.Errorf("texte du presse-papiers supérieur à la limite de %d octets (-max-size)", maxBytes)
	}
	if !utf8.ValidString(text) {
		return errors.New("le texte à copier n'est pas un UTF-8 valide")
	}
	if text == previous.text {
		return nil
	}
	// Reuse the original backend even if both Wayland and X11 are available.
	// A failing Wayland backend must never fall through to a different clipboard.
	backend, err := client.backend(previous.backend)
	if err != nil {
		return err
	}
	types, err := client.types(backend)
	if errors.Is(err, errClipboardNoText) {
		return errClipboardChanged
	}
	if err != nil {
		return err
	}
	if linuxClipboardHasNonTextSelection(types) || linuxClipboardTargetRank(previous.target, backend.name) < 0 {
		return errClipboardChanged
	}
	available := false
	for _, target := range types {
		available = available || target == previous.target
	}
	if !available {
		return errClipboardChanged
	}
	current, err := client.readTarget(backend, previous.target, maxBytes)
	if errors.Is(err, errClipboardNoText) {
		return errClipboardChanged
	}
	if err != nil {
		return err
	}
	if current != previous.text {
		return errClipboardChanged
	}

	// These utilities cannot provide an atomic compare-and-set. Rechecking the
	// same text/type immediately before taking ownership narrows the race.
	args := []string{"--type", "text/plain;charset=utf-8"}
	if backend.name == "xclip" {
		args = []string{"-selection", "clipboard", "-in", "-target", "UTF8_STRING", "-silent"}
	}
	_, err = client.run(backend.writer, args, &text, maxBytes)
	if err != nil {
		return fmt.Errorf("écriture du presse-papiers %s (vérifiez l'accès à la session graphique) : %w", backend.name, err)
	}
	return nil
}

// File managers can offer text/plain as a fallback for copied files. Likewise,
// some image owners offer an additional text flavor. Do not turn either into
// a text selection merely because a plain-text target happens to be present.
func linuxClipboardHasNonTextSelection(types []string) bool {
	for _, target := range types {
		kind := strings.ToLower(target)
		if mediaType, _, err := mime.ParseMediaType(target); err == nil {
			kind = mediaType
		}
		if strings.HasPrefix(kind, "image/") {
			return true
		}
		switch kind {
		case "text/uri-list", "x-special/gnome-copied-files", "x-special/mate-copied-files",
			"x-special/nautilus-clipboard", "application/x-kde-cutselection",
			"application/x-kde4-urilist", "application/x-moz-file", "application/x-qt-image":
			return true
		}
	}
	return false
}

func linuxClipboardTargetRank(target, backend string) int {
	if target == "UTF8_STRING" {
		if backend == "xclip" {
			return 0
		}
		return 2
	}
	mediaType, parameters, err := mime.ParseMediaType(target)
	if err != nil || mediaType != "text/plain" {
		return -1
	}
	charset := parameters["charset"]
	if charset == "" {
		return 3
	}
	if strings.EqualFold(charset, "utf-8") || strings.EqualFold(charset, "utf8") {
		return 1
	}
	// STRING/TEXT/COMPOUND_TEXT and non-UTF-8 charsets are not UTF-8 contracts.
	return -1
}

func linuxClipboardTextTarget(types []string, backend string) (string, error) {
	if linuxClipboardHasNonTextSelection(types) {
		return "", errClipboardNoText
	}
	target, best := "", 100
	for _, candidate := range types {
		if rank := linuxClipboardTargetRank(candidate, backend); rank >= 0 && rank < best {
			target, best = candidate, rank
		}
	}
	if target == "" {
		return "", errClipboardNoText
	}
	return target, nil
}

type linuxClipboardCommandError struct {
	program    string
	diagnostic string
	err        error
}

func (err *linuxClipboardCommandError) Error() string {
	if err.diagnostic == "" {
		return fmt.Sprintf("%s : %v", filepath.Base(err.program), err.err)
	}
	return fmt.Sprintf("%s : %v (%s)", filepath.Base(err.program), err.err, err.diagnostic)
}

func (err *linuxClipboardCommandError) Unwrap() error { return err.err }

func linuxClipboardNoSelection(err error, backend string) bool {
	var failure *linuxClipboardCommandError
	if !errors.As(err, &failure) {
		return false
	}
	diagnostic := strings.ToLower(failure.diagnostic)
	if backend == "wayland" {
		return strings.Contains(diagnostic, "nothing is copied") ||
			strings.Contains(diagnostic, "clipboard content is not available as requested type")
	}
	return strings.Contains(diagnostic, "there is no owner for the clipboard selection") ||
		strings.Contains(diagnostic, "target targets not available") ||
		strings.Contains(diagnostic, "target utf8_string not available") ||
		strings.Contains(diagnostic, "target string not available") ||
		strings.Contains(diagnostic, "cannot convert clipboard selection to target")
}

// A bounded writer stops a read subprocess as soon as it exceeds -max-size.
type linuxClipboardBuffer struct {
	buffer   bytes.Buffer
	maximum  int64
	cancel   context.CancelFunc
	exceeded bool
}

func (buffer *linuxClipboardBuffer) Write(data []byte) (int, error) {
	remaining := buffer.maximum - int64(buffer.buffer.Len())
	if int64(len(data)) <= remaining {
		return buffer.buffer.Write(data)
	}
	n, _ := buffer.buffer.Write(data[:int(remaining)])
	buffer.exceeded = true
	buffer.cancel()
	return n, errors.New("limite du presse-papiers dépassée")
}

type linuxClipboardDiagnostic struct{ buffer bytes.Buffer }

func (buffer *linuxClipboardDiagnostic) Write(data []byte) (int, error) {
	if remaining := linuxClipboardDiagnosticMax - buffer.buffer.Len(); remaining > 0 {
		part := data
		if len(part) > remaining {
			part = part[:remaining]
		}
		_, _ = buffer.buffer.Write(part)
	}
	// Continue draining after the diagnostic limit, without allocating more.
	return len(data), nil
}

func runLinuxClipboardCommand(program string, args []string, input *string, maximum int64, timeout time.Duration) ([]byte, error) {
	if maximum < 0 || input != nil && int64(len(*input)) > maximum {
		return nil, fmt.Errorf("limite du presse-papiers dépassée : %d octets maximum (-max-size)", maximum)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, program, args...)
	command.WaitDelay = linuxClipboardWaitDelay
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		// wl-paste may use a child process to transfer data. Stop the whole
		// command group on timeout/overflow, but never after successful exit.
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	output := linuxClipboardBuffer{maximum: maximum, cancel: cancel}
	var diagnostic linuxClipboardDiagnostic
	if input == nil {
		command.Stdout, command.Stderr = &output, &diagnostic
	} else {
		command.Stdin = strings.NewReader(*input)
		// wl-copy and xclip fork to remain clipboard owners. A Go-managed
		// stderr/stdout pipe inherited by that child would hold Wait open.
		devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return nil, err
		}
		defer devnull.Close()
		command.Stdout, command.Stderr = devnull, devnull
	}
	err := command.Run()
	if output.exceeded {
		return nil, fmt.Errorf("limite du presse-papiers dépassée : %d octets maximum (-max-size)", maximum)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s : délai de %s dépassé : %w", filepath.Base(program), timeout, ctx.Err())
		}
		return nil, &linuxClipboardCommandError{
			program: program, diagnostic: strings.TrimSpace(diagnostic.buffer.String()), err: err,
		}
	}
	return output.buffer.Bytes(), nil
}
