package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func runClipboardTest(args []string, ops clipboardOps) rmwmTestResult {
	var stdout, stderr bytes.Buffer
	code := runWithClipboard(args, strings.NewReader("unused stdin"), &stdout, &stderr, ops)
	return rmwmTestResult{code: code, stdout: stdout.Bytes(), stderr: stderr.Bytes()}
}

func TestClipboardCleanupUsesSnapshotAndKeepsTextPrivate(t *testing.T) {
	initial := clipboardSnapshot{text: "secret\u200B\r\n\tvalue\u00A0! —⸺⸻", sequence: 42, backend: "test", target: "text/plain"}
	reads, writes := 0, 0
	ops := clipboardOps{
		read: func(max int64) (clipboardSnapshot, error) {
			reads++
			if max != 64*1024*1024 {
				t.Fatalf("read limit = %d", max)
			}
			return initial, nil
		},
		replace: func(before clipboardSnapshot, after string, max int64) error {
			writes++
			if before != initial || after != "secret\r\n\tvalue ! ---" || max != 64*1024*1024 {
				t.Errorf("write did not preserve checked snapshot, exact line endings, or limit")
			}
			return nil
		},
	}
	result := runClipboardTest([]string{"~", "-stats"}, ops)
	rmwmTestCode(t, result, 0)
	if reads != 1 || writes != 1 {
		t.Fatalf("reads=%d writes=%d", reads, writes)
	}
	if len(result.stdout) != 0 || bytes.Contains(result.stderr, []byte("secret")) {
		t.Fatal("clipboard content appeared in output")
	}
	var stats report
	if err := json.Unmarshal(result.stderr, &stats); err != nil {
		t.Fatalf("invalid stats: %v: %s", err, result.stderr)
	}
	if stats.Scanned != 1 || stats.Changed != 1 || stats.Written != 1 || stats.Removed != 1 || stats.Replaced != 4 {
		t.Fatalf("incorrect report: %+v", stats)
	}
}

func TestClipboardChecksAndUnchangedContentNeverWrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		args []string
		code int
	}{
		{"check dirty", "a\u200Bb", []string{"~", "-check"}, 1},
		{"check clean", "a b", []string{"-check", "~"}, 0},
		{"dry run dirty", "a\u200Bb", []string{"~", "-dry-run"}, 0},
		{"unchanged", "a b\r\n", []string{"~"}, 0},
		{"empty text", "", []string{"~"}, 0},
		{"legitimate emoji", "👩‍💻 et ❤️", []string{"~"}, 0},
		{"spaces kept", "Bonjour\u00A0!", []string{"~", "-no-normalize-spaces"}, 0},
		{"check dashes", "A—B⸺C⸻D", []string{"~", "-check"}, 1},
		{"dashes kept", "A—B⸺C⸻D", []string{"~", "-no-normalize-dashes"}, 0},
		{"check dashes kept", "A—B⸺C⸻D", []string{"~", "-check", "-no-normalize-dashes"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops := clipboardOps{
				read: func(int64) (clipboardSnapshot, error) { return clipboardSnapshot{text: tc.text}, nil },
				replace: func(clipboardSnapshot, string, int64) error {
					t.Fatal("read-only or unchanged operation attempted a clipboard write")
					return nil
				},
			}
			result := runClipboardTest(tc.args, ops)
			rmwmTestCode(t, result, tc.code)
			if len(result.stdout) != 0 {
				t.Fatal("clipboard mode wrote to stdout")
			}
		})
	}
}

func TestClipboardRejectsInvalidDataBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		args []string
	}{
		{"UTF8", "a\xffb", []string{"~"}},
		{"NUL", "a\x00b", []string{"~"}},
		{"too large", strings.Repeat("x", 1024*1024+1), []string{"~", "-max-size=1"}},
		{"invalid JSON", `{broken}`, []string{"~", "-format=json"}},
		{"JSON collision", `{"key":1,"ke\u200by":2}`, []string{"~", "-format=json"}},
		{"JSON dash collision", `{"a-b":1,"a\u2e3ab":2}`, []string{"~", "-format=json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := runClipboardTest(tc.args, clipboardOps{
				read: func(int64) (clipboardSnapshot, error) { return clipboardSnapshot{text: tc.text}, nil },
				replace: func(clipboardSnapshot, string, int64) error {
					t.Fatal("invalid clipboard was overwritten")
					return nil
				},
			})
			rmwmTestCode(t, result, 2)
			if len(result.stdout) != 0 {
				t.Fatal("invalid clipboard was exposed on stdout")
			}
		})
	}
}

func TestClipboardNoTextAndReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
	}{
		{"image or empty clipboard", errClipboardNoText, 0},
		{"wrapped no text", errors.Join(errors.New("backend"), errClipboardNoText), 0},
		{"unavailable", errors.New("backend indisponible"), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := runClipboardTest([]string{"~", "-check"}, clipboardOps{
				read: func(int64) (clipboardSnapshot, error) { return clipboardSnapshot{}, tc.err },
				replace: func(clipboardSnapshot, string, int64) error {
					t.Fatal("non-text or inaccessible clipboard was written")
					return nil
				},
			})
			rmwmTestCode(t, result, tc.code)
		})
	}
}

func TestClipboardConcurrentCopyIsReportedWithoutRetry(t *testing.T) {
	writes := 0
	result := runClipboardTest([]string{"~", "-stats"}, clipboardOps{
		read: func(int64) (clipboardSnapshot, error) { return clipboardSnapshot{text: "a\u200Bb", sequence: 9}, nil },
		replace: func(clipboardSnapshot, string, int64) error {
			writes++
			return errClipboardChanged
		},
	})
	rmwmTestCode(t, result, 2)
	if writes != 1 || !bytes.Contains(result.stderr, []byte("nouvelle copie conservée")) || !bytes.Contains(result.stderr, []byte(`"files_written":0`)) {
		t.Fatalf("concurrent modification not reported safely: writes=%d stderr=%s", writes, result.stderr)
	}
}

func TestClipboardFormatDispatch(t *testing.T) {
	original := "{\r\n  \"message\": \"Bon\\u200bjour\", \"number\": 1.00e+02\r\n}"
	for _, format := range []string{"auto", "text", "json"} {
		t.Run(format, func(t *testing.T) {
			writes := 0
			result := runClipboardTest([]string{"~", "-format=" + format, "-quiet"}, clipboardOps{
				read: func(int64) (clipboardSnapshot, error) { return clipboardSnapshot{text: original}, nil },
				replace: func(before clipboardSnapshot, after string, _ int64) error {
					writes++
					if before.text != original || after != strings.Replace(original, `\u200b`, "", 1) {
						t.Error("JSON cleanup changed unrelated lexical formatting")
					}
					return nil
				},
			})
			rmwmTestCode(t, result, 0)
			wantWrites := 0
			if format == "json" {
				wantWrites = 1
			}
			if writes != wantWrites || len(result.stdout) != 0 || len(result.stderr) != 0 {
				t.Fatalf("format=%s writes=%d stdout=%s stderr=%s", format, writes, result.stdout, result.stderr)
			}
		})
	}
}

func TestClipboardAndBackgroundArgumentErrorsNeverAccessDesktop(t *testing.T) {
	for _, args := range [][]string{
		{"~", "file.md"}, {"~", "-"}, {"~", "~"}, {"~", "-o", "result.txt"},
		{"-bg", "~"}, {"-bg", "-"}, {"-bg", "file.md"}, {"-bg", "-check"},
		{"-bg", "-dry-run"}, {"-bg", "-stats"}, {"-bg", "-o", "result.txt"},
		{"-bg", "-bg-stop"}, {"-bg-worker"}, {"-bg-stop", "~"},
		{"~", "-check", "-dry-run"}, {"~", "-max-size=0"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			// Empty operations deliberately panic if validation reaches the OS.
			result := runClipboardTest(args, clipboardOps{})
			rmwmTestCode(t, result, 2)
		})
	}
}

func TestBackgroundDispatchAndOptions(t *testing.T) {
	for _, mode := range []string{"start", "worker", "stop"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			check := func(cfg config) error {
				calls++
				if !cfg.background || cfg.NormalizeSpaces || cfg.NormalizeDashes || !cfg.StripBidi || !cfg.StripEmojiGlue || !cfg.AggressiveHomoglyphs || cfg.format != "json" || cfg.maxBytes != 2*1024*1024 {
					t.Fatalf("cleaner options were lost in background dispatch: %+v", cfg)
				}
				return nil
			}
			args := []string{"-bg", "-no-normalize-spaces", "-no-normalize-dashes", "-strip-bidi", "-strip-emoji-glue", "-aggressive-homoglyphs", "-format=json", "-max-size=2"}
			ops := clipboardOps{}
			switch mode {
			case "start":
				ops.startMonitor = check
			case "worker":
				args = append(args, "-bg-worker")
				ops.monitor = check
			case "stop":
				args = []string{"-bg-stop"}
				ops.stopMonitor = func() error { calls++; return nil }
			}
			result := runClipboardTest(args, ops)
			rmwmTestCode(t, result, 0)
			if calls != 1 || len(result.stdout) != 0 {
				t.Fatalf("calls=%d stdout=%s", calls, result.stdout)
			}
		})
	}
}

func TestBackgroundStartFailureIsNotReportedAsSuccess(t *testing.T) {
	result := runClipboardTest([]string{"-bg"}, clipboardOps{
		startMonitor: func(config) error { return errors.New("initialisation refusée") },
	})
	rmwmTestCode(t, result, 2)
	if bytes.Contains(result.stderr, []byte("surveillance du presse-papiers active")) {
		t.Fatal("failed background launch was reported as active")
	}
}

func TestClipboardLeadingFEFFIsTextAndIsCleaned(t *testing.T) {
	cfg := config{CleanOptions: CleanOptions{NormalizeSpaces: true}, format: "auto", maxBytes: 1024}
	cleaned, stats, err := cleanClipboardText(cfg, "\uFEFFBonjour\r\n")
	if err != nil || cleaned != "Bonjour\r\n" || stats.Removed != 1 {
		t.Fatalf("clipboard FEFF treated as a file BOM: cleaned=%q stats=%+v err=%v", cleaned, stats, err)
	}
}

func TestClipboardJSONErrorsDoNotExposeKeys(t *testing.T) {
	result := runClipboardTest([]string{"~", "-format=json"}, clipboardOps{
		read: func(int64) (clipboardSnapshot, error) {
			return clipboardSnapshot{text: `{"private-key":1,"private-ke\u200by":2}`}, nil
		},
	})
	rmwmTestCode(t, result, 2)
	if bytes.Contains(result.stderr, []byte("private")) || len(result.stdout) != 0 {
		t.Fatal("clipboard JSON diagnostic exposed content")
	}
}
