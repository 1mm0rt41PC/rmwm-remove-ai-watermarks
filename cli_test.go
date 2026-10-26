package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type rmwmTestResult struct {
	code   int
	stdout []byte
	stderr []byte
}

func rmwmTestRun(args []string, input []byte) rmwmTestResult {
	var stdout, stderr bytes.Buffer
	code := run(args, bytes.NewReader(input), &stdout, &stderr)
	return rmwmTestResult{code, stdout.Bytes(), stderr.Bytes()}
}

func rmwmTestCode(t *testing.T, result rmwmTestResult, want int) {
	t.Helper()
	if result.code != want {
		t.Fatalf("exit code = %d, want %d; stdout=%q; stderr=%q", result.code, want, result.stdout, result.stderr)
	}
}

func TestStdinIsExactFilter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no added newline", "alpha\u200B beta", "alpha beta"},
		{"empty stream", "", ""},
		{"CRLF and indentation", "# Title\r\n\r\n    code\u200B()\r\n\titem\r\n", "# Title\r\n\r\n    code()\r\n\titem\r\n"},
		{"UTF8 BOM", "\uFEFFalpha\u200B beta\r\n", "\uFEFFalpha beta\r\n"},
		{"long dashes", "a—b⸺c⸻d\r\n\t– − ‐ -", "a-b-c-d\r\n\t– − ‐ -"},
		{"long line", strings.Repeat("x", 128*1024) + "\u200B", strings.Repeat("x", 128*1024)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rmwmTestRun([]string{"-"}, []byte(tt.input))
			rmwmTestCode(t, result, 0)
			if !bytes.Equal(result.stdout, []byte(tt.want)) {
				t.Fatalf("stdout does not exactly match cleaned input (got %d bytes, want %d)", len(result.stdout), len(tt.want))
			}
			if len(result.stderr) != 0 {
				t.Errorf("default filter emitted stderr: %q", result.stderr)
			}
		})
	}
}

func TestStdinStatsStayOnStderr(t *testing.T) {
	result := rmwmTestRun([]string{"-", "--stats"}, []byte("alpha\u200B beta"))
	rmwmTestCode(t, result, 0)
	if string(result.stdout) != "alpha beta" {
		t.Fatalf("stats contaminated stdout: %q", result.stdout)
	}
	if len(result.stderr) == 0 {
		t.Error("--stats did not produce a report on stderr")
	}
}

func TestStdinCheckExitCodes(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		code  int
	}{
		{"clean", "alpha beta", 0},
		{"change needed", "alpha\u200B beta", 1},
		{"dashes only", "a—b⸺c⸻d", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := rmwmTestRun([]string{"-", "--check"}, []byte(tt.input))
			rmwmTestCode(t, result, tt.code)
		})
	}
}

func TestDashNormalizationOption(t *testing.T) {
	input := "\tA—B⸺C⸻D\r\n"
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"preserve", []string{"-", "-no-normalize-dashes"}, input},
		{"explicit false", []string{"--no-normalize-dashes=false", "-"}, "\tA-B-C-D\r\n"},
		{"check preserved", []string{"-", "-check", "-no-normalize-dashes"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := rmwmTestRun(tt.args, []byte(input))
			rmwmTestCode(t, result, 0)
			if string(result.stdout) != tt.want {
				t.Fatalf("stdout=%q, want=%q", result.stdout, tt.want)
			}
		})
	}
}

func TestDashNormalizationFileCheckAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashes.md")
	original := []byte("# Titre\r\nA—B⸺C⸻D\r\n")
	rmwmTestWrite(t, path, original)
	result := rmwmTestRun([]string{path, "-check"}, nil)
	rmwmTestCode(t, result, 1)
	rmwmTestFile(t, path, original)
	rmwmTestAbsent(t, path+".bak")

	result = rmwmTestRun([]string{path}, nil)
	rmwmTestCode(t, result, 0)
	rmwmTestFile(t, path, []byte("# Titre\r\nA-B-C-D\r\n"))
	rmwmTestFile(t, path+".bak", original)
	rmwmTestCode(t, rmwmTestRun([]string{path, "-check"}, nil), 0)
}

func TestUsageErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{"missing path", nil},
		{"unknown option", []string{"--this-option-does-not-exist", "-"}},
		{"missing output value", []string{"-", "-o"}},
		{"missing size value", []string{"-", "--max-size"}},
		{"invalid size value", []string{"-", "--max-size=abc"}},
		{"negative size value", []string{"-", "--max-size=-1"}},
		{"missing extension value", []string{"-", "--ext"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := rmwmTestRun(tt.args, []byte("alpha\u200B beta"))
			rmwmTestCode(t, result, 2)
			if len(result.stderr) == 0 {
				t.Error("invalid invocation did not explain the error on stderr")
			}
			if len(result.stdout) != 0 {
				t.Errorf("invalid invocation emitted stdout: %q", result.stdout)
			}
		})
	}
}

func TestHelp(t *testing.T) {
	result := rmwmTestRun([]string{"--help"}, nil)
	rmwmTestCode(t, result, 0)
	if !bytes.Contains(append(result.stdout, result.stderr...), []byte("rmwm")) {
		t.Error("help does not identify rmwm")
	}
}

func TestExtensionOptionFormsAndPlacement(t *testing.T) {
	for _, tt := range []struct {
		name string
		args func(string) []string
	}{
		{"short equals after path", func(dir string) []string { return []string{dir, "-ext=TXT,.YAML"} }},
		{"long separate before path", func(dir string) []string { return []string{"--ext", ".txt,YAML", dir} }},
		{"long equals after path", func(dir string) []string { return []string{dir, "--ext=.TXT,yaml"} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			included := []string{"a.txt", "b.TXT", "c.yaml", "d.YAML"}
			dirty := []byte("{\"text\":\"a\u200Bb\"}")
			clean := []byte("{\"text\":\"ab\"}")
			for _, name := range append(append([]string{}, included...), "excluded.md", "excluded.json") {
				rmwmTestWrite(t, filepath.Join(dir, name), dirty)
			}
			result := rmwmTestRun(tt.args(dir), nil)
			rmwmTestCode(t, result, 0)
			for _, name := range included {
				rmwmTestFile(t, filepath.Join(dir, name), clean)
			}
			for _, name := range []string{"excluded.md", "excluded.json"} {
				rmwmTestFile(t, filepath.Join(dir, name), dirty)
			}
		})
	}
}

func TestOutputFromFileAndStdin(t *testing.T) {
	for _, fromStdin := range []bool{false, true} {
		name := "file"
		if fromStdin {
			name = "stdin"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source.md")
			output := filepath.Join(dir, "result.md")
			original := []byte("alpha\u200B beta\r\n")
			var args []string
			var input []byte
			if fromStdin {
				args, input = []string{"-", "-o", output}, original
			} else {
				rmwmTestWrite(t, source, original)
				args = []string{source, "-o", output}
			}
			result := rmwmTestRun(args, input)
			rmwmTestCode(t, result, 0)
			rmwmTestFile(t, output, []byte("alpha beta\r\n"))
			if !fromStdin {
				rmwmTestFile(t, source, original)
				rmwmTestAbsent(t, source+".bak")
			}
			if bytes.Contains(result.stdout, []byte("alpha beta")) {
				t.Errorf("-o also copied the cleaned content to stdout: %q", result.stdout)
			}
		})
	}
}

func TestJSONModeDispatchForFilesAndStdin(t *testing.T) {
	original := []byte("{\r\n  \"text\": \"a\\u200Bb\",\r\n  \"number\": 1.2300e+02\r\n}")
	want := []byte("{\r\n  \"text\": \"ab\",\r\n  \"number\": 1.2300e+02\r\n}")
	path := filepath.Join(t.TempDir(), "document.JSON")
	rmwmTestWrite(t, path, original)
	result := rmwmTestRun([]string{path}, nil)
	rmwmTestCode(t, result, 0)
	rmwmTestFile(t, path, want)
	rmwmTestFile(t, path+".bak", original)
	result = rmwmTestRun([]string{"-", "--format=json"}, original)
	rmwmTestCode(t, result, 0)
	if !bytes.Equal(result.stdout, want) {
		t.Fatalf("JSON filter stdout=%q, want=%q", result.stdout, want)
	}
	// Without a JSON hint, stdin is plain text and literal escape sequences stay literal.
	result = rmwmTestRun([]string{"-"}, original)
	rmwmTestCode(t, result, 0)
	if !bytes.Equal(result.stdout, original) {
		t.Fatalf("text filter interpreted JSON escape sequences: %q", result.stdout)
	}
}

func TestQuietDoesNotSuppressFilteredText(t *testing.T) {
	for _, flag := range []string{"--quiet", "-q"} {
		t.Run(flag, func(t *testing.T) {
			result := rmwmTestRun([]string{"-", flag}, []byte("a\u200Bb"))
			rmwmTestCode(t, result, 0)
			if string(result.stdout) != "ab" || len(result.stderr) != 0 {
				t.Fatalf("quiet filter stdout=%q stderr=%q", result.stdout, result.stderr)
			}
		})
	}
}

func TestStdinRejectsInvalidAndOversizedInputWithoutPartialOutput(t *testing.T) {
	for _, tt := range []struct {
		name  string
		args  []string
		input []byte
	}{
		{"invalid UTF8", []string{"-"}, []byte{'a', 0xFF, 'b'}},
		{"binary NUL", []string{"-"}, []byte{'a', 0, 'b'}},
		{"oversized", []string{"-", "--max-size=1"}, bytes.Repeat([]byte{'x'}, 1024*1024+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := rmwmTestRun(tt.args, tt.input)
			rmwmTestCode(t, result, 2)
			if len(result.stdout) != 0 {
				t.Fatalf("rejected input produced partial output: %d bytes", len(result.stdout))
			}
			if len(result.stderr) == 0 {
				t.Error("rejected input did not explain the error")
			}
		})
	}
}

func TestMissingPathReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.md")
	result := rmwmTestRun([]string{path}, nil)
	rmwmTestCode(t, result, 2)
	if len(result.stderr) == 0 {
		t.Error("missing path did not produce an error message")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("missing input path was created or cannot be inspected: %v", err)
	}
}
