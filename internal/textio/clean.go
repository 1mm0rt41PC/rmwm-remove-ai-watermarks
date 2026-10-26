package textio

import (
	"path/filepath"
	"strings"
)

// ************************************************************************************************
// Clean preserves encoding and unchanged bytes while applying the supplied text
// or JSON cleaner. Generic options and statistics keep this I/O package independent
// of the public library. Callers validate format before invoking this function.
func Clean[Options, Stats any](data []byte, name, format string, options Options,
	cleanText func(string, Options) (string, Stats),
	cleanJSON func(string, Options) (string, Stats, error),
) ([]byte, Stats, error) {
	var zero Stats
	text, encoding, err := mk_decodeText(data)
	if err != nil {
		return nil, zero, err
	}
	var cleaned string
	var stats Stats
	if format == "json" || format == "auto" && strings.EqualFold(filepath.Ext(name), ".json") {
		cleaned, stats, err = cleanJSON(text, options)
	} else {
		cleaned, stats = cleanText(text, options)
	}
	if err != nil {
		return nil, zero, err
	}
	if text == cleaned {
		return data, stats, nil
	}
	return mk_encodeText(cleaned, encoding), stats, nil
}
