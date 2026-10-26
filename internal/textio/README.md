# Internal text I/O

Encoding and document I/O shared by [internal/cleaner](../cleaner) and the command, plus file safety
helpers for one-shot CLI cleanup. The library does not monitor files or directories.
This package has no dependency on either caller. It is internal, not a supported
public module API.

## Declaration index

| Declaration | Purpose |
| --- | --- |
| [Encoding](files.go:15) | Supported encoding identifier |
| [UTF8, UTF8BOM, UTF16LE, UTF16BE](files.go:17) | Encoding variants |
| [DecodeText](files.go:26) | Decode and validate textual-input policy |
| [DecodeTextBytes](files.go:40) | Decode without reapplying input policy to cleaned output |
| [ValidateText](files.go:87) | Reject recognized containers and non-text controls |
| [EncodeText](files.go:102) | Restore the original encoding and BOM |
| [ReadLimited](files.go:124) | Read with a byte limit |
| [WriteAll](files.go:135) | Report writer failures and short writes |
| [SourceSnapshot](files.go:146) | Source path, metadata and original bytes |
| [ReadSource](files.go:152) | Read a regular file with identity and mutation checks |
| [WriteNewFile](files.go:204) | Create a new output exclusively |
| [WriteNewFileProtected](files.go:208) | Copy source protection before writing |
| [ReplaceSource](files.go:264) | Recheck, back up and replace through platform helpers |
| [Clean](clean.go:12) | Decode, select supplied text/JSON cleaner and preserve encoding |
| [ParseExtensions](filter.go:11) | Validate a case-insensitive extension set |
| [ExcludedDirectory](filter.go:30) | Shared dependency/version-control exclusion policy |

The cleaning helper accepts caller-owned option/statistics types and callbacks;
Unicode and JSON implementations live only in [internal/cleaner](../cleaner), behind
the root [facade](../../export.go). This package imports neither. Clipboard UTF-16
decoding stays in the command because its NUL-terminated input contract differs
from file encoding.

Snapshots detect observed concurrent changes but do not lock other processes out.
Windows replacement retains native DACL/recovery behavior; Unix uses a same-directory
rename. Existing backups and separate output files are never overwritten.
