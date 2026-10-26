# Internal cleaner

Implementation of the public root [facade](../../export.go). This package owns
stream validation/cleanup, Unicode rules, JSON preservation, and their unit tests.
External callers import the module root, not this internal package.

The dependency direction is root facade → cleaner → [textio](../textio).
The command imports the root facade and textio. Neither internal package imports
the root or command, and textio receives cleaning callbacks rather than importing
cleaner. No cleaning logic is duplicated in the facade.

## Declaration index

| Declaration | Purpose |
| --- | --- |
| [Opt](api.go:13) | Explicit stream options |
| [RunDry](api.go:26) | Changed physical line pairs |
| [HasWatermarks](api.go:52) | Detect changes under selected rules |
| [CleanupWatermarks](api.go:64) | Validate and clean input before writing |
| [CleanOptions](unicode.go:17) | Low-level Unicode options |
| [CleanStats](unicode.go:26) | Code-point statistics |
| [CleanText](unicode.go:38) | Context-sensitive Unicode cleanup |
| [CleanJSON](json.go:20) | Lexically preserving JSON cleanup |
| [ErrNilArgument](errors.go:8) | Nil reader/writer sentinel |
| [ErrInvalidFormat](errors.go:9) | Unsupported format sentinel |

Implementation tests and their helper live here; external-package examples and
facade smoke tests remain at the root. CLI and clipboard behavior stays in the
command. There is no filesystem monitoring.
