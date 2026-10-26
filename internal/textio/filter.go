package textio

import (
	"errors"
	"fmt"
	"strings"
)

// ************************************************************************************************
// ParseExtensions validates a comma-separated, case-insensitive extension set.
func ParseExtensions(value string) (map[string]bool, error) {
	result := make(map[string]bool)
	for _, part := range strings.Split(value, ",") {
		ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(part), "."))
		if ext == "" {
			return nil, errors.New("-ext contient une extension vide")
		}
		for _, r := range ext {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				return nil, fmt.Errorf("extension invalide dans -ext : %q", part)
			}
		}
		result["."+ext] = true
	}
	return result, nil
}

// ************************************************************************************************
// ExcludedDirectory identifies dependency, environment and version-control directories.
func ExcludedDirectory(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".hg", ".svn", "node_modules", "vendor", ".venv", "venv":
		return true
	}
	return false
}
