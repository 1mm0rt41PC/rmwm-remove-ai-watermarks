//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type linuxClipboardStep struct {
	program string
	args    []string
	input   *string
	limit   int64
	output  string
	err     error
}

func scriptedLinuxClipboard(t *testing.T, environment map[string]string, steps []linuxClipboardStep) linuxClipboardClient {
	t.Helper()
	position := 0
	t.Cleanup(func() {
		if position != len(steps) {
			t.Errorf("only %d of %d clipboard commands ran", position, len(steps))
		}
	})
	return linuxClipboardClient{
		getenv: func(name string) string { return environment[name] },
		lookPath: func(name string) (string, error) {
			return filepath.Join("/clipboard-tools", name), nil
		},
		run: func(program string, args []string, input *string, limit int64) ([]byte, error) {
			t.Helper()
			if position >= len(steps) {
				t.Fatalf("unexpected clipboard command: %s %q", program, args)
			}
			step := steps[position]
			position++
			if filepath.Base(program) != step.program || !reflect.DeepEqual(args, step.args) {
				t.Fatalf("command %d: want %s %q; got %s %q", position, step.program, step.args, program, args)
			}
			if !reflect.DeepEqual(input, step.input) {
				t.Fatalf("command %d: stdin differs", position)
			}
			if limit != step.limit {
				t.Fatalf("command %d: want limit %d; got %d", position, step.limit, limit)
			}
			return []byte(step.output), step.err
		},
	}
}

func TestLinuxClipboardWaylandPreservesWhitespaceAndWritesStdin(t *testing.T) {
	const limit = 1024
	const target = "text/plain;charset=utf-8"
	const types = "text/html\ntext/plain\n" + target + "\nUTF8_STRING\n"
	original := "  a\u200bb\r\n\n"
	cleaned := "  ab\r\n\n"
	client := scriptedLinuxClipboard(t, map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":1"}, []linuxClipboardStep{
		{program: "wl-paste", args: []string{"--list-types"}, limit: linuxClipboardTypesLimit, output: types},
		{program: "wl-paste", args: []string{"--no-newline", "--type", target}, limit: limit, output: original},
		{program: "wl-paste", args: []string{"--list-types"}, limit: linuxClipboardTypesLimit, output: types},
		{program: "wl-paste", args: []string{"--no-newline", "--type", target}, limit: limit, output: original},
		{program: "wl-copy", args: []string{"--type", target}, input: &cleaned, limit: limit},
	})
	snapshot, err := client.read(limit)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.text != original || snapshot.target != target || snapshot.backend != "wayland" {
		t.Fatalf("unexpected clipboard snapshot: %+v", snapshot)
	}
	if err := client.replace(snapshot, cleaned, limit); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxClipboardX11UsesClipboardAndUTF8(t *testing.T) {
	const limit = 1024
	const types = "TARGETS\nTIMESTAMP\ntext/plain;charset=utf-8\nUTF8_STRING\nSTRING\n"
	original := "$(not-a-command) \"quoted\" 'text'\u200b\n"
	cleaned := "$(not-a-command) \"quoted\" 'text'\n"
	client := scriptedLinuxClipboard(t, map[string]string{"DISPLAY": ":1", "XDG_SESSION_TYPE": "x11"}, []linuxClipboardStep{
		{program: "xclip", args: []string{"-selection", "clipboard", "-out", "-target", "TARGETS"}, limit: linuxClipboardTypesLimit, output: types},
		{program: "xclip", args: []string{"-selection", "clipboard", "-out", "-target", "UTF8_STRING"}, limit: limit, output: original},
		{program: "xclip", args: []string{"-selection", "clipboard", "-out", "-target", "TARGETS"}, limit: linuxClipboardTypesLimit, output: types},
		{program: "xclip", args: []string{"-selection", "clipboard", "-out", "-target", "UTF8_STRING"}, limit: limit, output: original},
		{program: "xclip", args: []string{"-selection", "clipboard", "-in", "-target", "UTF8_STRING", "-silent"}, input: &cleaned, limit: limit},
	})
	snapshot, err := client.read(limit)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.text != original || snapshot.backend != "xclip" || snapshot.target != "UTF8_STRING" {
		t.Fatalf("unexpected clipboard snapshot: %+v", snapshot)
	}
	if err := client.replace(snapshot, cleaned, limit); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxClipboardSkipsImagesFilesAndNonPlainText(t *testing.T) {
	for name, types := range map[string]string{
		"image":          "image/png\n",
		"image and text": "image/png\ntext/plain\n",
		"file URI list":  "text/uri-list\ntext/plain;charset=utf-8\n",
		"GNOME files":    "x-special/gnome-copied-files\nUTF8_STRING\n",
		"rich text only": "text/html\ntext/rtf\n",
		"legacy charset": "text/plain;charset=iso-8859-1\nSTRING\nCOMPOUND_TEXT\n",
		"empty offer":    "TARGETS\nTIMESTAMP\n",
	} {
		t.Run(name, func(t *testing.T) {
			client := scriptedLinuxClipboard(t, map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, []linuxClipboardStep{
				{program: "wl-paste", args: []string{"--list-types"}, limit: linuxClipboardTypesLimit, output: types},
			})
			if _, err := client.read(1024); !errors.Is(err, errClipboardNoText) {
				t.Fatalf("wanted no text, got %v", err)
			}
		})
	}
}

func TestLinuxClipboardRejectsChangedTextOrTargetsBeforeWriting(t *testing.T) {
	previous := clipboardSnapshot{text: "a\u200bb\n", backend: "wayland", target: "text/plain"}
	for name, steps := range map[string][]linuxClipboardStep{
		"text changed": {
			{program: "wl-paste", args: []string{"--list-types"}, limit: linuxClipboardTypesLimit, output: "text/plain\n"},
			{program: "wl-paste", args: []string{"--no-newline", "--type", "text/plain"}, limit: 1024, output: "new user copy\n"},
		},
		"format changed": {
			{program: "wl-paste", args: []string{"--list-types"}, limit: linuxClipboardTypesLimit, output: "text/plain;charset=utf-8\n"},
		},
		"changed to files": {
			{program: "wl-paste", args: []string{"--list-types"}, limit: linuxClipboardTypesLimit, output: "text/plain\ntext/uri-list\n"},
		},
		"cleared": {
			{program: "wl-paste", args: []string{"--list-types"}, limit: linuxClipboardTypesLimit,
				err: &linuxClipboardCommandError{program: "wl-paste", diagnostic: "Nothing is copied", err: errors.New("exit status 1")}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			client := scriptedLinuxClipboard(t, map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, steps)
			if err := client.replace(previous, "ab\n", 1024); !errors.Is(err, errClipboardChanged) {
				t.Fatalf("wanted clipboard changed, got %v", err)
			}
		})
	}
}

func TestLinuxClipboardReplaceUsesRecordedBackendAndTarget(t *testing.T) {
	original := "a\u200bb"
	cleaned := "ab"
	previous := clipboardSnapshot{text: original, backend: "xclip", target: "text/plain"}
	client := scriptedLinuxClipboard(t, map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":1"}, []linuxClipboardStep{
		{program: "xclip", args: []string{"-selection", "clipboard", "-out", "-target", "TARGETS"}, limit: linuxClipboardTypesLimit, output: "UTF8_STRING\ntext/plain\n"},
		{program: "xclip", args: []string{"-selection", "clipboard", "-out", "-target", "text/plain"}, limit: 1024, output: original},
		{program: "xclip", args: []string{"-selection", "clipboard", "-in", "-target", "UTF8_STRING", "-silent"}, input: &cleaned, limit: 1024},
	})
	if err := client.replace(previous, cleaned, 1024); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxClipboardDoesNotFallbackFromWayland(t *testing.T) {
	client := scriptedLinuxClipboard(t, map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":1"}, nil)
	client.lookPath = func(name string) (string, error) {
		if name == "xclip" {
			t.Fatal("must not look for xclip after selecting Wayland")
		}
		return "", exec.ErrNotFound
	}
	if _, err := client.read(1024); err == nil || !strings.Contains(err.Error(), "wl-clipboard") {
		t.Fatalf("wanted dependency error, got %v", err)
	}

	client = scriptedLinuxClipboard(t, map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":1"}, []linuxClipboardStep{
		{program: "wl-paste", args: []string{"--list-types"}, limit: linuxClipboardTypesLimit,
			err: &linuxClipboardCommandError{program: "wl-paste", diagnostic: "Failed to connect to a Wayland server", err: errors.New("exit status 1")}},
	})
	if _, err := client.read(1024); err == nil || errors.Is(err, errClipboardNoText) {
		t.Fatalf("unavailable display is an error, not an empty clipboard: %v", err)
	}
}

func TestLinuxClipboardRequiresGraphicalSession(t *testing.T) {
	client := scriptedLinuxClipboard(t, nil, nil)
	if _, err := client.read(1024); err == nil || !strings.Contains(err.Error(), "session") {
		t.Fatalf("wanted graphical-session error, got %v", err)
	}
}

func TestLinuxClipboardEmptyAndMissingSelection(t *testing.T) {
	for _, test := range []struct{ backend, diagnostic string }{
		{"wayland", "Nothing is copied\n"},
		{"xclip", "Error: target TARGETS not available\n"},
		{"xclip", "xclip: Error: There is no owner for the CLIPBOARD selection\n"},
	} {
		err := &linuxClipboardCommandError{program: test.backend, diagnostic: test.diagnostic, err: errors.New("exit status 1")}
		if !linuxClipboardNoSelection(err, test.backend) {
			t.Errorf("did not recognize absent selection: %q", test.diagnostic)
		}
	}
	client := scriptedLinuxClipboard(t, map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, []linuxClipboardStep{
		{program: "wl-paste", args: []string{"--list-types"}, limit: linuxClipboardTypesLimit, output: "text/plain\n"},
		{program: "wl-paste", args: []string{"--no-newline", "--type", "text/plain"}, limit: 1024, output: ""},
	})
	previous, err := client.read(1024)
	if err != nil || previous.text != "" {
		t.Fatalf("empty text selection: %+v, %v", previous, err)
	}
	if err := client.replace(previous, "", 1024); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxClipboardRefusesOversizedAndInvalidUTF8(t *testing.T) {
	for name, content := range map[string]string{"large": "12345", "invalid UTF-8": string([]byte{0xff})} {
		t.Run(name, func(t *testing.T) {
			client := scriptedLinuxClipboard(t, map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, []linuxClipboardStep{
				{program: "wl-paste", args: []string{"--list-types"}, limit: linuxClipboardTypesLimit, output: "text/plain\n"},
				{program: "wl-paste", args: []string{"--no-newline", "--type", "text/plain"}, limit: 4, output: content},
			})
			if _, err := client.read(4); err == nil {
				t.Fatal("invalid clipboard text was accepted")
			}
		})
	}
	client := scriptedLinuxClipboard(t, nil, nil)
	for _, text := range []string{"12345", string([]byte{0xff})} {
		if err := client.replace(clipboardSnapshot{}, text, 4); err == nil {
			t.Fatal("invalid replacement should fail before any clipboard command")
		}
	}
}

// This helper is the test executable itself, never a real clipboard utility.
// Its child briefly retains stdio descriptors to model a clipboard owner.
const linuxClipboardHelperText = " $(not-a-command) 'quoted' \"text\" café\n\n"

func TestLinuxClipboardProcessHelper(t *testing.T) {
	if os.Getenv("RMWM_LINUX_CLIPBOARD_TEST_HELPER") != "1" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "output":
		_, _ = io.WriteString(os.Stdout, linuxClipboardHelperText)
	case "large":
		for {
			if _, err := io.WriteString(os.Stdout, strings.Repeat("x", 8192)); err != nil {
				os.Exit(3)
			}
		}
	case "diagnostic":
		_, _ = io.WriteString(os.Stderr, strings.Repeat("x", linuxClipboardDiagnosticMax*4))
		os.Exit(3)
	case "wait":
		time.Sleep(3 * time.Second)
	case "writer":
		data, err := io.ReadAll(os.Stdin)
		if err != nil || string(data) != linuxClipboardHelperText {
			os.Exit(4)
		}
		// The background process must inherit real /dev/null descriptors,
		// not Go's output-copying pipes.
		for _, file := range []*os.File{os.Stdout, os.Stderr} {
			info, err := file.Stat()
			if err != nil || info.Mode()&os.ModeCharDevice == 0 {
				os.Exit(5)
			}
		}
		child := exec.Command(os.Args[0], "-test.run=^TestLinuxClipboardProcessHelper$", "--", "hold-stdio")
		child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(6)
		}
		_ = child.Process.Release()
	case "hold-stdio":
		time.Sleep(500 * time.Millisecond)
	default:
		os.Exit(7)
	}
	os.Exit(0)
}

func linuxClipboardHelperCommand(t *testing.T, mode string) (string, []string) {
	t.Helper()
	t.Setenv("RMWM_LINUX_CLIPBOARD_TEST_HELPER", "1")
	// A race-instrumented helper otherwise waits one second in os.Exit,
	// which is unrelated to clipboard ownership or command timeouts.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return program, []string{"-test.run=^TestLinuxClipboardProcessHelper$", "--", mode}
}

func TestLinuxClipboardCommandRunnerPreservesBytes(t *testing.T) {
	program, args := linuxClipboardHelperCommand(t, "output")
	data, err := runLinuxClipboardCommand(program, args, nil, int64(len(linuxClipboardHelperText)), time.Second)
	if err != nil || string(data) != linuxClipboardHelperText {
		t.Fatalf("clipboard bytes differ: %q, %v", data, err)
	}
}

func TestLinuxClipboardCommandRunnerStopsAtSizeLimit(t *testing.T) {
	program, args := linuxClipboardHelperCommand(t, "large")
	data, err := runLinuxClipboardCommand(program, args, nil, 32, time.Second)
	if err == nil || !strings.Contains(err.Error(), "limite") || data != nil {
		t.Fatalf("expected bounded output failure without partial text: %d bytes, %v", len(data), err)
	}
}

func TestLinuxClipboardCommandRunnerBoundsDiagnostic(t *testing.T) {
	program, args := linuxClipboardHelperCommand(t, "diagnostic")
	_, err := runLinuxClipboardCommand(program, args, nil, 1024, time.Second)
	var failure *linuxClipboardCommandError
	if !errors.As(err, &failure) || len(failure.diagnostic) != linuxClipboardDiagnosticMax {
		t.Fatalf("diagnostic was not bounded: %v", err)
	}
}

func TestLinuxClipboardCommandRunnerTimesOut(t *testing.T) {
	program, args := linuxClipboardHelperCommand(t, "wait")
	_, err := runLinuxClipboardCommand(program, args, nil, 1024, 50*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wanted deadline error, got %v", err)
	}
}

func TestLinuxClipboardWriterCanRemainOwnerInBackground(t *testing.T) {
	program, args := linuxClipboardHelperCommand(t, "writer")
	input := linuxClipboardHelperText
	_, err := runLinuxClipboardCommand(program, args, &input, 1024, time.Second)
	if err != nil {
		t.Fatal(fmt.Errorf("writer parent should complete while owner child retains stdio: %w", err))
	}
}
