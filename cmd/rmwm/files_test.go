package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf16"
)

func rmwmTestWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func rmwmTestFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q, want %q", path, got, want)
	}
}

func rmwmTestAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s should not exist; lstat error=%v", path, err)
	}
}

func TestInPlaceBackupPreservesOriginalAndExistingBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "document.md")
	original := []byte("# Title\r\n    alpha\u200B beta\r\n")
	previousBackup := []byte("an earlier backup\x00must remain byte-identical")
	rmwmTestWrite(t, path, original)
	rmwmTestWrite(t, path+".bak", previousBackup)
	result := rmwmTestRun([]string{path}, nil)
	rmwmTestCode(t, result, 0)
	rmwmTestFile(t, path, []byte("# Title\r\n    alpha beta\r\n"))
	rmwmTestFile(t, path+".bak", previousBackup)
	rmwmTestFile(t, path+".bak.1", original)
	result = rmwmTestRun([]string{path}, nil)
	rmwmTestCode(t, result, 0)
	rmwmTestAbsent(t, path+".bak.2")
	// A second pass over already cleaned input must retain both historical copies.
	rmwmTestFile(t, path+".bak", previousBackup)
	rmwmTestFile(t, path+".bak.1", original)
}

func TestNoBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.md")
	rmwmTestWrite(t, path, []byte("a\u200Bb"))
	result := rmwmTestRun([]string{path, "--no-backup"}, nil)
	rmwmTestCode(t, result, 0)
	rmwmTestFile(t, path, []byte("ab"))
	rmwmTestAbsent(t, path+".bak")
}

func TestCleanFileIsNotRewritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.md")
	original := []byte("# A clean document\r\n\r\n    keep indentation\r\n")
	rmwmTestWrite(t, path, original)
	oldTime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	result := rmwmTestRun([]string{path}, nil)
	rmwmTestCode(t, result, 0)
	rmwmTestFile(t, path, original)
	rmwmTestAbsent(t, path+".bak")
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("clean file was rewritten: mtime before=%v after=%v", before.ModTime(), after.ModTime())
	}
}

func TestCheckAndDryRunNeverWriteFiles(t *testing.T) {
	for _, tt := range []struct {
		name string
		flag string
		code int
	}{
		{"check", "--check", 1},
		{"dry run", "--dry-run", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "document.md")
			original := []byte("a\u200Bb\r\n")
			rmwmTestWrite(t, path, original)
			oldTime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
			if err := os.Chtimes(path, oldTime, oldTime); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			result := rmwmTestRun([]string{dir, tt.flag}, nil)
			rmwmTestCode(t, result, tt.code)
			rmwmTestFile(t, path, original)
			rmwmTestAbsent(t, path+".bak")
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !after.ModTime().Equal(before.ModTime()) {
				t.Error("read-only mode changed file mtime")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Errorf("read-only mode created files: %v", entries)
			}
		})
	}
}

func TestDirectoryDefaultsAndExcludedDirectories(t *testing.T) {
	dir := t.TempDir()
	dirty := []byte("{\"text\":\"a\u200Bb\"}")
	clean := []byte("{\"text\":\"ab\"}")
	included := []string{"readme.md", "UPPER.MD", "data.json", "upper.JSON", "child/nested.md", "child/nested.json"}
	excluded := []string{"text.txt", "config.yaml", "archive.md.bak", "archive.json.bak.1", "no_extension"}
	for _, name := range []string{".git", ".hg", ".svn", "node_modules", "vendor", ".venv", "venv"} {
		excluded = append(excluded, filepath.Join(name, "ignored.md"))
	}
	for _, name := range append(append([]string{}, included...), excluded...) {
		rmwmTestWrite(t, filepath.Join(dir, name), dirty)
	}
	result := rmwmTestRun([]string{dir}, nil)
	rmwmTestCode(t, result, 0)
	for _, name := range included {
		path := filepath.Join(dir, name)
		rmwmTestFile(t, path, clean)
		rmwmTestFile(t, path+".bak", dirty)
	}
	for _, name := range excluded {
		path := filepath.Join(dir, name)
		rmwmTestFile(t, path, dirty)
		rmwmTestAbsent(t, path+".bak")
	}
}

func TestNoRecursiveAndExplicitUnknownExtension(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.md")
	childPath := filepath.Join(dir, "child", "nested.md")
	otherPath := filepath.Join(dir, "explicit.custom")
	dirty := []byte("a\u200Bb")
	for _, path := range []string{rootPath, childPath, otherPath} {
		rmwmTestWrite(t, path, dirty)
	}
	result := rmwmTestRun([]string{dir, "--no-recursive"}, nil)
	rmwmTestCode(t, result, 0)
	rmwmTestFile(t, rootPath, []byte("ab"))
	rmwmTestFile(t, childPath, dirty)
	rmwmTestAbsent(t, childPath+".bak")
	rmwmTestFile(t, otherPath, dirty)
	result = rmwmTestRun([]string{otherPath}, nil)
	rmwmTestCode(t, result, 0)
	rmwmTestFile(t, otherPath, []byte("ab"))
	rmwmTestFile(t, otherPath+".bak", dirty)
}

func rmwmTestUTF16(text string, order binary.ByteOrder, withBOM bool) []byte {
	units := utf16.Encode([]rune(text))
	if withBOM {
		units = append([]uint16{0xFEFF}, units...)
	}
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		order.PutUint16(data[i*2:], unit)
	}
	return data
}

func TestEncodingBOMAndLineEndingsSurviveInPlace(t *testing.T) {
	dirty := "# Title\r\n    café\u200B 漢字 🐙\r\n"
	clean := "# Title\r\n    café 漢字 🐙\r\n"
	for _, tt := range []struct {
		name   string
		encode func(string) []byte
	}{
		{"UTF8", func(s string) []byte { return []byte(s) }},
		{"UTF8 BOM", func(s string) []byte { return append([]byte{0xEF, 0xBB, 0xBF}, []byte(s)...) }},
		{"UTF16 LE BOM", func(s string) []byte { return rmwmTestUTF16(s, binary.LittleEndian, true) }},
		{"UTF16 BE BOM", func(s string) []byte { return rmwmTestUTF16(s, binary.BigEndian, true) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "document.md")
			original := tt.encode(dirty)
			rmwmTestWrite(t, path, original)
			result := rmwmTestRun([]string{path}, nil)
			rmwmTestCode(t, result, 0)
			rmwmTestFile(t, path, tt.encode(clean))
			rmwmTestFile(t, path+".bak", original)
		})
	}
}

func TestInvalidFileIsUntouchedAndOtherFilesStillProcessed(t *testing.T) {
	for _, tt := range []struct {
		name string
		data []byte
	}{
		{"invalid UTF8", []byte{'a', 0xFF, 'b'}},
		{"binary", []byte{'a', 0, 'b'}},
		{"UTF16 LE without BOM", rmwmTestUTF16("text", binary.LittleEndian, false)},
		{"UTF16 BE without BOM", rmwmTestUTF16("text", binary.BigEndian, false)},
		{"odd UTF16 length", []byte{0xFF, 0xFE, 'a'}},
		{"unpaired UTF16 surrogate", []byte{0xFF, 0xFE, 0x00, 0xD8}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			invalid := filepath.Join(dir, "a-invalid.md")
			valid := filepath.Join(dir, "z-valid.md")
			rmwmTestWrite(t, invalid, tt.data)
			rmwmTestWrite(t, valid, []byte("a\u200Bb"))
			result := rmwmTestRun([]string{dir}, nil)
			rmwmTestCode(t, result, 2)
			rmwmTestFile(t, invalid, tt.data)
			rmwmTestAbsent(t, invalid+".bak")
			rmwmTestFile(t, valid, []byte("ab"))
			if len(result.stderr) == 0 {
				t.Error("invalid file was not reported")
			}
		})
	}
}

func TestFileMaxSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.md")
	original := bytes.Repeat([]byte{'x'}, 1024*1024+1)
	rmwmTestWrite(t, path, original)
	result := rmwmTestRun([]string{path, "--max-size", "1"}, nil)
	rmwmTestCode(t, result, 2)
	rmwmTestFile(t, path, original)
	rmwmTestAbsent(t, path+".bak")
}

func TestCheckCleanDirectoryAndErrorPriority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "document.md")
	rmwmTestWrite(t, path, []byte("already clean\r\n"))
	result := rmwmTestRun([]string{dir, "--check"}, nil)
	rmwmTestCode(t, result, 0)
	rmwmTestAbsent(t, path+".bak")
	dirty := []byte("a\u200Bb")
	invalid := []byte{'a', 0xFF, 'b'}
	rmwmTestWrite(t, path, dirty)
	invalidPath := filepath.Join(dir, "invalid.md")
	rmwmTestWrite(t, invalidPath, invalid)
	result = rmwmTestRun([]string{dir, "--check"}, nil)
	// An unreadable/invalid document has priority over "changes needed".
	rmwmTestCode(t, result, 2)
	rmwmTestFile(t, path, dirty)
	rmwmTestFile(t, invalidPath, invalid)
	rmwmTestAbsent(t, path+".bak")
	rmwmTestAbsent(t, invalidPath+".bak")
}

func TestJSONKeyCollisionNeverRewritesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collision.json")
	original := []byte(`{"name":1,"na\u200Bme":2}`)
	rmwmTestWrite(t, path, original)
	result := rmwmTestRun([]string{path}, nil)
	rmwmTestCode(t, result, 2)
	rmwmTestFile(t, path, original)
	rmwmTestAbsent(t, path+".bak")
	if len(result.stderr) == 0 {
		t.Error("key collision was not explained")
	}
}

func TestDirectoryNeverFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	external := t.TempDir()
	target := filepath.Join(external, "target.md")
	dirty := []byte("a\u200Bb")
	rmwmTestWrite(t, target, dirty)
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(external, filepath.Join(dir, "linked-directory")); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(dir, "local.md")
	rmwmTestWrite(t, local, dirty)
	result := rmwmTestRun([]string{dir}, nil)
	rmwmTestCode(t, result, 0)
	rmwmTestFile(t, local, []byte("ab"))
	rmwmTestFile(t, target, dirty)
	rmwmTestAbsent(t, target+".bak")
	rmwmTestAbsent(t, link+".bak")
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symbolic link was replaced: info=%v error=%v", info, err)
	}
	// Explicit symlinks may be skipped or rejected, but their targets must never be modified.
	result = rmwmTestRun([]string{link}, nil)
	if result.code != 0 && result.code != 2 {
		t.Errorf("explicit symlink returned unexpected code %d", result.code)
	}
	rmwmTestFile(t, target, dirty)
	rmwmTestAbsent(t, target+".bak")
	rmwmTestAbsent(t, link+".bak")
}
