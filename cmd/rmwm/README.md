# rmwm command

This directory contains the complete executable and command tests. The command
imports the root [facade](../../export.go) for Unicode and JSON cleaning and the internal text I/O
package for encoding, file safety, and traversal filters. No command declarations
are exposed as a library API.

## Source index

| Source | Responsibility |
| --- | --- |
| [main.go](main.go) | Process entry and exit status |
| [cli.go](cli.go) | Flags, defaults, file/stream processing and reports |
| [clipboard.go](clipboard.go) | Clipboard decisions and injectable desktop operations |
| [clipboard_windows.go](clipboard_windows.go) | Native Windows clipboard access |
| [clipboard_linux.go](clipboard_linux.go) | Wayland/X11 utility integration |
| [clipboard_other.go](clipboard_other.go) | Unsupported-platform errors |
| [clipboard_utf16.go](clipboard_utf16.go) | Clipboard-specific NUL-terminated UTF-16 decoding |
| [background.go](background.go) | Worker argument construction |
| [background_windows.go](background_windows.go) | Windows launcher, notification loop and confirmation UI |
| [background_other.go](background_other.go) | Unsupported-platform background errors |
| [mock.go](mock.go) | Shared cleaner test seam |

Tests use injected clipboard operations, not the user's clipboard. Build and release
instructions are in the [root README](../../README.md); binaries are uploaded
manually to releases, not committed to source.
