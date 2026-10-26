# rmwm — Remove AI Watermarks

[![Go](https://img.shields.io/badge/Go-1.26.10%2B-blue.svg)](https://go.dev/dl/)
[![Version](https://img.shields.io/badge/version-0.3.0-blue.svg)](https://github.com/1mm0rt41PC/rmwm-remove-ai-watermarks/releases)

A local Go library and command-line tool for cleaning invisible and unusual Unicode characters from text files, JSON strings, standard input, and the clipboard.

**Repository:** https://github.com/1mm0rt41PC/rmwm-remove-ai-watermarks

The executable is called **rmwm**. It has no external Go dependencies and makes no network requests at runtime. Linux clipboard support requires local clipboard utilities; Windows clipboard support is native.

**Scope:** this is deterministic Unicode cleanup, not AI detection. Targeted characters can occur in human-written text. The tool does not guarantee removal of statistical AI watermarks and does not process image watermarks, Office/PDF containers, or C2PA metadata.

## Contents

- [Download and installation](#download-and-installation)
- [Usage](#usage)
- [Go library](#go-library)
- [Clipboard](#clipboard)
- [Windows clipboard monitoring](#windows-clipboard-monitoring)
- [Options and exit codes](#options-and-exit-codes)
- [Cleaning rules](#cleaning-rules)
- [JSON handling](#json-handling)
- [File safety and encodings](#file-safety-and-encodings)
- [Build and test](#build-and-test)
- [Source reference](#source-reference)
- [Origin and limitations](#origin-and-limitations)

## Download and installation

Download available prebuilt binaries **only from [GitHub Releases](https://github.com/1mm0rt41PC/rmwm-remove-ai-watermarks/releases)**. 

Windows, from the directory containing the downloaded executable:

```powershell
.\rmwm.exe -version
.\rmwm.exe C:\my\dir -check
.\rmwm.exe C:\my\dir
```

Linux, from the directory containing the downloaded executable:

```sh
chmod +x ./rmwm
./rmwm -version
./rmwm ./documents -check
./rmwm ./documents
```

The examples below assume the executable is on your search path.

## Usage

### Directories

```powershell
# Recursively clean Markdown and JSON files (the default extensions).
rmwm C:\my\dir

# Replace the default extension list; flags may precede or follow paths.
rmwm C:\my\dir -ext="md,json,txt"
rmwm -ext=".MD,.JSON" C:\my\dir

# Process only the top-level directory.
rmwm "C:\My documents" -no-recursive
```

The extension filter applies only to directory traversal. Extensions are case-insensitive, may have a leading dot, and may have surrounding whitespace. Empty lists, empty entries, and glob patterns are rejected.

Nested version-control directories, dependency directories, and virtual environments are skipped: `.git`, `.hg`, `.svn`, `node_modules`, `vendor`, `.venv`, and `venv`. Symbolic links are not followed; a symbolic link supplied directly as input is rejected.

### Files

```powershell
# Clean in place, preserving the original in a backup if content changes.
rmwm C:\my\dir\myfile.md

# Explicit files are accepted regardless of their extension.
rmwm C:\my\dir\notes.txt

# Write a new file without modifying the input.
rmwm C:\my\dir\myfile.md -o C:\my\dir\myfile.cleaned.md

# Write cleaned content to standard output.
rmwm C:\my\dir\myfile.md -o -
```

A separate output file must not already exist. If the output is exactly the input path, in-place rules apply. The output option requires one input and cannot be combined with a directory, check mode, or dry-run mode.

Multiple file or directory paths can be processed together. Errors are reported while independent inputs continue; the final exit code is then 2. Directory processing is not a transaction: files already cleaned remain changed.

### Standard input

```sh
rmwm - < input.md > cleaned.md
rmwm -format=json - < input.json > cleaned.json
```

Standard output contains only the cleaned bytes, without a banner, statistics, or an added newline. Diagnostics go to standard error. Quiet mode does not suppress cleaned output.

Automatic format selection uses the input file extension. Standard input and the clipboard default to text, even when an output filename ends in `.json`. Select JSON explicitly to clean escaped Unicode in a pipeline.

**Never redirect output to the file being read:** the shell can truncate it before the program starts. Use in-place processing instead.

PowerShell pipelines can decode and re-encode text before it reaches the program, especially in Windows PowerShell 5.1. Direct file processing avoids this. For byte-preserving redirection from PowerShell:

```powershell
cmd /d /c 'rmwm - < "C:\my\dir\input.md" > "C:\my\dir\cleaned.md"'
cmd /d /c 'rmwm -format=json - < "C:\my\dir\input.json" > "C:\my\dir\cleaned.json"'
```

## Go library

Import the repository root as package **rmwm**. The executable entry point is [cmd/rmwm/main.go](cmd/rmwm/main.go).

### Package architecture

- [export.go](export.go) is the only production Go file at the root: it aliases the public types, forwards the five public functions, and reexports the same sentinel errors. It contains no cleaning or I/O implementation. Public examples and facade smoke tests stay at the root.
- [internal/cleaner](internal/cleaner) owns the reader APIs, Unicode/JSON rules, implementation tests, and test helpers. It depends on shared text I/O, never on the root facade or command. It has no filesystem monitoring, command parser, command configuration, terminal reporting, clipboard backend, or background clipboard worker.
- [cmd/rmwm](cmd/rmwm) owns the complete CLI and its tests, including file/stream commands, flags, exit codes, reports, clipboard access, and Windows background UI. It imports the root library; the library never imports the command.
- [internal/textio](internal/textio) is the single shared implementation of encoding/BOM handling, textual-input validation, bounded reads, complete writes, snapshots, exclusive backups, protected copies, platform replacement, and directory filters. Its encoded-document helper receives the library cleaners as callbacks, so it does not import the root library and cannot create an import cycle.

Command behavior is not a public library API. Use the reader APIs below from Go, or invoke the executable for one-shot file/directory cleanup and other command-line behavior. Only the clipboard supports monitoring, through the Windows CLI. Library and CLI defaults remain independent.

```sh
go get github.com/1mm0rt41PC/rmwm-remove-ai-watermarks
```

### Clean, detect, and preview streams

```go
package main

import (
    "bytes"
    "fmt"
    "strings"

    rmwm "github.com/1mm0rt41PC/rmwm-remove-ai-watermarks"
)

func main() {
    input := "Hello\u200b world—again\n"
    options := rmwm.Opt{NormalizeSpaces: true, NormalizeDashes: true}
    found, err := rmwm.HasWatermarks(strings.NewReader(input), options)
    if err != nil { panic(err) }
    fmt.Println("Cleanup needed:", found)

    changes, err := rmwm.RunDry(strings.NewReader(input), options)
    if err != nil { panic(err) }
    for _, change := range changes { fmt.Printf("%s\n", change) }

    var output bytes.Buffer
    if err := rmwm.CleanupWatermarks(strings.NewReader(input), &output, options); err != nil {
        panic(err)
    }
    fmt.Print(output.String())
}
```

Each call consumes its reader without closing it. Detection means that the selected cleanup rules would change the bytes, **not** that AI authorship or a watermark has been proven. Encoding and BOM handling are the same as for files. Automatic stream format is always text; explicitly select JSON to interpret escaped Unicode.

The reader APIs read and validate the complete input before returning changes or writing output. Invalid encoding, invalid JSON, duplicate keys (including cleanup collisions), and read failures produce no partial cleaned output. Write failures and short writes are returned, but an arbitrary writer may already have accepted bytes; the library cannot roll those back. Use a separate output buffer or file when transactional output is needed. Reader APIs do not close or flush the caller's streams and have no implicit size limit.

### Dry-run parts

[RunDry](export.go:38) returns only changed physical lines, in input order, each encoded as `before\n---\nafter`. Cleaning and validation happen on the whole document first, preserving context-sensitive Unicode rules. Lines are paired by their original position, not by a diff algorithm, so repeated lines and lines that become empty stay aligned. The separating LF is excluded from each line; CR from a CRLF line is retained. A final LF does not add a spurious change. Previews are decoded Unicode strings without the encoding BOM, even for UTF-16 input.

For JSON, these are **serialized document lines**, not individual decoded keys or values. A changed JSON string may have different escape spelling. Unchanged lines are omitted; clean input returns no parts. The delimiter is a display convention, not a reversible patch format or a unique separator for arbitrary input.

### Library options

All fields belong to [Opt](export.go:13). Unlike CLI defaults, **every boolean defaults to false**.

| Field | Meaning / zero value |
| --- | --- |
| Format | Empty or `auto`: streams are text; also accepts `text` and `json` |
| NormalizeSpaces | Normalize unusual spaces |
| NormalizeDashes | Normalize the three supported em-dash variants |
| StripBidi | Remove otherwise-preserved bidi controls |
| StripEmojiGlue | Remove otherwise-preserved emoji/script glue |
| NormalizeHomoglyphs | Enable the existing aggressive homoglyph mapping |

## Clipboard

```powershell
# Read, clean, and replace clipboard text.
rmwm '~'

# Check or preview without changing the clipboard.
rmwm '~' -check
rmwm '~' -dry-run

# Clean escaped Unicode in copied JSON.
rmwm '~' -format=json

# Preserve nonbreaking spaces or the three normalized dashes.
rmwm '~' -no-normalize-spaces
rmwm '~' -no-normalize-dashes
```

In Bash/Zsh, quote the tilde to prevent home-directory expansion. To process an actual file named tilde, use a relative path with a directory prefix or an absolute path.

Clipboard mode cannot be combined with other inputs or an output path. It never prints clipboard content to standard output. Statistics go to standard error. Unchanged content is not rewritten; unsupported non-text content is skipped.

**Cleaning replaces clipboard content with plain text.** Rich HTML/RTF representations are not preserved and there is no clipboard backup. Line endings and final newlines are preserved. In text mode, an initial U+FEFF is cleaned as text rather than preserved as a file encoding marker. Strict JSON mode rejects that prefix without writing.

The clipboard is reread before replacement to avoid overwriting a newer copy. Windows checks and writes under the same clipboard lock. Linux rechecks reduce this risk, but external clipboard ownership transfer is not atomic across applications.

### Linux desktop requirements

| Session | Required utilities | Selection |
| --- | --- | --- |
| Wayland, including GNOME on Wayland | `wl-paste` and `wl-copy` from `wl-clipboard` | Standard clipboard |
| X11, including XFCE or GNOME on Xorg | `xclip` | `CLIPBOARD`, never `PRIMARY` |

On Debian/Ubuntu, install the appropriate backend:

```sh
sudo apt install wl-clipboard  # Wayland
sudo apt install xclip         # X11
rmwm '~'
```

Run as the desktop session user from a terminal in that session. Wayland is selected when its display variable is present or the session type is Wayland; otherwise an X11 display enables the X11 backend. A failing Wayland backend does not silently fall back to X11. Missing utilities or inaccessible displays produce an error.

Linux clipboard offers containing image or file formats are skipped, even if they also contain plain text. HTML/RTF plus plain text remains eligible when no image/file format is present. Legacy X11-only encodings are not implicitly converted to UTF-8. Clipboard utilities may remain in the background to serve copied text after rmwm exits.

## Windows clipboard monitoring

```powershell
# Start a background worker and release the terminal.
rmwm -bg

# Stop the worker in the current Windows session.
rmwm -bg-stop

# Restart with different cleanup options.
rmwm -bg -no-normalize-spaces -no-normalize-dashes
```

The worker uses native clipboard notifications and a hidden window, not polling or a Windows service. No startup entry or registry modification is installed. Only one worker runs per session; stop it before changing options.

The current clipboard is checked once at startup. Whenever supported text would change, a native Yes/No dialog asks for confirmation, with **No selected by default**. It shows removal/replacement counts and warns about plain-text conversion without displaying copied content. Declining does not cause repeated prompts for unchanged content; the worker does not loop on its own writes.

The dialog does not hold the clipboard lock. Before writing, the worker rechecks both the sequence number and content under lock. Changes are coalesced so only one dialog appears at a time. Invalid, oversized, or unsupported content is skipped. The size limit also applies to monitoring.

The launcher hides its own console only when no other process shares it. A shared PowerShell or Command Prompt console remains visible. The worker has no visible console; launching the executable directly may briefly show a console before it is hidden.

Background start/stop cannot be combined with inputs, output paths, check mode, dry-run, or statistics. Quiet mode suppresses startup/stop messages, not confirmation dialogs. Monitoring is Windows-only.

## Options and exit codes

Options accept one or two leading hyphens and may appear before or after paths. Use the end-of-options marker before paths beginning with a hyphen.

| Option | Behavior |
| --- | --- |
| `-ext="md,json"` | Directory extension filter |
| `-o PATH`, `-output PATH` | Write a new file; use `-` for standard output |
| `-format=auto` | Select `auto`, `text`, or `json` |
| `-check` | Do not write; return 1 if cleanup is needed |
| `-dry-run` | Preview without writing; return 0 unless an error occurs |
| `-no-backup` | Do not retain an in-place backup |
| `-no-recursive` | Process only the directory root |
| `-no-normalize-spaces` | Preserve unusual Unicode spaces |
| `-no-normalize-dashes` | Preserve em dash, two-em dash, and three-em dash |
| `-strip-bidi` | Also remove otherwise-preserved directional controls |
| `-strip-emoji-glue` | Also remove invisible characters useful to emoji/languages |
| `-aggressive-homoglyphs` | Map a limited set of lookalike characters to ASCII |
| `-max-size=64` | Maximum input size in MiB |
| `-bg`, `-bg-stop` | Start/stop Windows clipboard monitoring |
| `-stats` | Write JSON statistics to standard error |
| `-quiet`, `-q` | Suppress ordinary messages |
| `-version` | Print version |
| `-help`, `-h` | Print command help |

The README is in English; current command-line diagnostics and help are in French.

| Exit code | Meaning |
| --- | --- |
| 0 | Success; in check mode, no changes needed |
| 1 | Check mode only: at least one input would change |
| 2 | Argument, input, format, read, or write error |

Errors take precedence over exit code 1. Check and dry-run modes never write cleaned content, including to standard output, and create no backups or temporary output files.

Statistics include scanned, changed, written, and skipped inputs; removed/replaced characters; and errors/warnings. The file counters also count the single clipboard input in clipboard mode. Scanned inputs count successfully validated reads. Detailed errors remain on standard error.

## Cleaning rules

Context-sensitive rules remove isolated zero-width spaces, soft hyphens, selected directional controls, orphan tags, noncharacters, reserved ignorables, and private-use characters. Recognized emoji joins, useful script-specific invisible characters, flag sequences, and supported legitimate bidirectional controls are preserved by default.

In the CLI, unusual Unicode spaces, including nonbreaking and narrow nonbreaking spaces, become ordinary spaces by default. Library booleans are explicit and default to false. Version 0.3.0 also supports normalizing each of these characters to **one ASCII hyphen**:

| Character | Code point | Replacement |
| --- | --- | --- |
| — | U+2014 | - |
| ⸺ | U+2E3A | - |
| ⸻ | U+2E3B | - |

En dashes, minus signs, and other punctuation remain unchanged. Space runs, indentation, line endings, and the presence or absence of a final newline are preserved.

Space and dash normalization can be disabled independently. Additional bidi stripping, emoji-glue stripping, and aggressive homoglyph mapping are opt-in. Homoglyph mapping covers a limited table of Cyrillic confusables and fullwidth Latin letters, not arbitrary transliteration.

The same rules apply to files, JSON keys and values, standard input, and the clipboard. Markdown code blocks receive no special protection: this is Unicode cleanup, not a Markdown parser. A detected change is not evidence of AI authorship or a watermark.

## JSON handling

JSON inputs must contain one strict JSON document. Keys and string values are decoded before cleaning, including Unicode escapes and surrogate pairs:

```json
{"message":"Hel\u200blo\u00a0!","version":1.00e+2}
```

becomes:

```json
{"message":"Hello !","version":1.00e+2}
```

Bytes outside changed strings remain identical: indentation, line endings, key order, and number spelling are retained. Changed strings are re-encoded as valid JSON; their escape spelling may change. Unchanged strings retain their original lexical representation. An escaped literal backslash followed by a Unicode escape name remains literal text.

Duplicate keys, including collisions introduced by cleaning, are rejected within each object. Invalid JSON and unpaired surrogates are rejected without writing. JSONC, JSONL, and concatenated JSON documents are not supported in JSON mode.

## File safety and encodings

An in-place change saves the original bytes in a backup with a `.bak` suffix. Existing backups are never overwritten: numbered suffixes are used instead. Clean inputs are not rewritten and create no backup.

Output is prepared and synchronized in a temporary file in the same directory before replacement. File identity and content are checked again before replacement. These checks detect observed changes but do not provide an interprocess lock.

On Windows, native file replacement preserves supported attributes, including the destination DACL. Copies receive the source DACL before content is written. A temporary recovery copy may be used even when retained backups are disabled; it is removed after successful replacement. Failures report retained recovery paths. Locks held by other applications can prevent replacement.

On Unix, permission bits are copied and replacement uses a same-filesystem rename. Backups preserve content, not every kind of metadata, extended POSIX ACL, ownership, extended attribute, EFS encryption, or alternate stream.

Supported encodings are UTF-8 with or without BOM and UTF-16 LE/BE with BOM. Encoding, the initial file BOM, line endings, and final newline presence are preserved. An internal U+FEFF remains subject to cleaning.

Invalid encodings, BOM-less UTF-16, UTF-32, recognized binary container signatures, NUL/DEL, and C0 controls other than tab, LF, CR, and FF are rejected. Convert legacy encodings such as Windows-1252 explicitly before processing; no lossy decoding is attempted.

The CLI default limit is **64 MiB per file, stream, or clipboard text**. Library reader APIs have no implicit size limit. Each input is loaded in full, so conversion can use more memory than its input size. This is not a constant-memory streaming filter; long lines are not subject to a 64 KiB line limit.

## Build and test

Requires **Go 1.26.10 or newer**.

```sh
git clone https://github.com/1mm0rt41PC/rmwm-remove-ai-watermarks.git
cd rmwm-remove-ai-watermarks
go test ./...
go vet ./...
```

Build on Windows:

```powershell
go build -trimpath -ldflags="-s -w" -o rmwm.exe ./cmd/rmwm
```

Build on Linux/macOS:

```sh
go build -trimpath -ldflags='-s -w' -o rmwm ./cmd/rmwm
```

Cross-compile Windows x64 from Linux/macOS:

```sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o rmwm.exe ./cmd/rmwm
```

Explicit output names keep the command named rmwm despite the longer repository name. Local executables and release output directories are ignored by Git. Maintainers upload binaries manually to [Releases](https://github.com/1mm0rt41PC/rmwm-remove-ai-watermarks/releases), never to the source tree.

Tests cover CLI behavior, Unicode cleanup, JSON preservation, backups, and clipboard decisions without modifying a real clipboard. Linux backend tests use simulated commands. Desktop dialogs and clipboard interoperability still require interactive validation on the target platform. [VALIDATION.md](VALIDATION.md) contains the historical validation report and manual checks, in French; it is not a claim that every environment has been tested.

## Source reference

The root is an importable Go library; [cmd/rmwm/main.go](cmd/rmwm/main.go) is the executable entry point. Exported production declarations are indexed below.

| Declaration | Purpose |
| --- | --- |
| [Opt](export.go:13) | Alias of the internal stream cleanup options; fields listed above |
| [RunDry](export.go:38) | Read and validate input, returning changed line pairs without writing |
| [HasWatermarks](export.go:45) | Report whether the selected cleanup changes input |
| [CleanupWatermarks](export.go:53) | Validate and clean a reader into a writer, preserving encoding |
| [CleanOptions](export.go:18) | Alias of the low-level Unicode cleanup settings |
| [CleanStats](export.go:22) | Alias of the Unicode code-point counters, not byte counts |
| [CleanText](export.go:63) | Apply context-sensitive Unicode cleanup to text |
| [CleanJSON](export.go:75) | Clean JSON keys and strings while preserving unchanged lexical content |

Low-level [CleanText](export.go:63) expects valid UTF-8 and does not perform file encoding/container validation. [CleanStats](export.go:22) exposes Removed, Replaced, InputRunes, and OutputRunes counters. [CleanOptions](export.go:18) exposes NormalizeSpaces, NormalizeDashes, StripEmojiGlue, StripBidi, and AggressiveHomoglyphs; all default to false.

Exported sentinel error variables in [export.go](export.go) retain the internal errors' identity for standard error matching:

| Variable | Meaning |
| --- | --- |
| [ErrNilArgument](export.go:28) | Nil reader or writer |
| [ErrInvalidFormat](export.go:31) | Unsupported format |

Other I/O and validation errors are returned as well; not every error has a sentinel. There are no exported production constants. Reader APIs validate the format and apply the selected cleanup rules. The module remains standard-library-only, so it does not depend on the private logging library used by other projects.

## Origin and limitations

The Unicode engine is derived from the MIT-licensed [watermarks-remover](https://github.com/guillaumemeyer/watermarks-remover), specifically its [Unicode cleaning implementation](https://github.com/guillaumemeyer/watermarks-remover/blob/980c0b4c5786ca710ed77276b6e55b622f340d4c/service/scripts/text_unicode.py) at revision 980c0b4c5786ca710ed77276b6e55b622f340d4c. Upstream attribution is retained in [internal/cleaner/unicode.go](internal/cleaner/unicode.go:1).

File/directory/stdin/clipboard interfaces, Windows monitoring, lexical JSON processing, and dash normalization are specific to this Go port. Upstream NFKC normalization is omitted to avoid external dependencies. Isolated Mongolian variation selectors are intentionally removed in one pass; legitimate reference sequences remain preserved.

The initial port was compared against 3,885 upstream test calls, excluding NFKC, with matching text and counters. Historical fuzzing completed 224,762 executions without failure. Those results predate the port-specific dash normalization and do not certify the absence of defects or watermarks.

Model-based rewriting, statistical watermark removal, images, Office/PDF processing, C2PA metadata, and Markdown/YAML/HTML metadata removal are outside this tool's scope.
