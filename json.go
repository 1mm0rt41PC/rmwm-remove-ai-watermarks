package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// CleanJSON cleans decoded JSON strings, including object keys and Unicode
// escapes. Bytes outside changed string tokens are kept exactly as supplied.
// Changed strings are re-encoded as JSON, so their original escape spelling can
// change. InputRunes and OutputRunes count the complete serialized documents;
// Removed and Replaced count characters in the decoded strings.
//
// Invalid UTF-8, invalid JSON, unpaired UTF-16 surrogate escapes, and duplicate
// object keys after cleaning are errors. Duplicate keys already present in the
// input are rejected too. On error no partial result is returned.
func CleanJSON(text string, opts CleanOptions) (string, CleanStats, error) {
	if !utf8.ValidString(text) {
		return "", CleanStats{}, fmt.Errorf("JSON input is not valid UTF-8")
	}
	if !json.Valid([]byte(text)) {
		// Unmarshal into RawMessage reports the syntax error without decoding
		// numbers or creating a tree of the entire input document.
		var raw json.RawMessage
		err := json.Unmarshal([]byte(text), &raw)
		return "", CleanStats{}, fmt.Errorf("invalid JSON: %w", err)
	}

	stats := CleanStats{InputRunes: utf8.RuneCountInString(text)}
	var output strings.Builder
	copiedUntil := 0
	var objects []map[string]int

	for pos := 0; pos < len(text); {
		switch text[pos] {
		case '{':
			objects = append(objects, make(map[string]int))
			pos++
		case '}':
			objects[len(objects)-1] = nil
			objects = objects[:len(objects)-1]
			pos++
		case '"':
			end, err := jsonStringEnd(text, pos)
			if err != nil {
				return "", CleanStats{}, err
			}
			var decoded string
			if err := json.Unmarshal([]byte(text[pos:end]), &decoded); err != nil {
				return "", CleanStats{}, fmt.Errorf("invalid JSON string at byte %d: %w", pos+1, err)
			}
			cleaned, part := CleanText(decoded, opts)

			// In a valid JSON document only object keys are followed by a
			// colon. Keeping only object scopes also handles arrays of objects.
			if jsonStringIsKey(text, end) {
				keys := objects[len(objects)-1]
				if previous, exists := keys[cleaned]; exists {
					return "", CleanStats{}, fmt.Errorf(
						"duplicate JSON object key %q after cleaning (bytes %d and %d)",
						cleaned, previous+1, pos+1,
					)
				}
				keys[cleaned] = pos
			}

			stats.Removed += part.Removed
			stats.Replaced += part.Replaced
			if cleaned != decoded {
				encoded, err := encodeJSONString(cleaned)
				if err != nil {
					return "", CleanStats{}, err
				}
				output.WriteString(text[copiedUntil:pos])
				output.WriteString(encoded)
				copiedUntil = end
			}
			pos = end
		default:
			pos++
		}
	}

	if copiedUntil == 0 {
		stats.OutputRunes = stats.InputRunes
		return text, stats, nil
	}
	output.WriteString(text[copiedUntil:])
	result := output.String()
	stats.OutputRunes = utf8.RuneCountInString(result)
	return result, stats, nil
}

// jsonStringEnd receives a token in a document already checked by json.Valid.
// encoding/json accepts unpaired surrogate escapes and replaces them with
// U+FFFD; reject them explicitly before decoding to avoid silent data loss.
func jsonStringEnd(text string, start int) (int, error) {
	for pos := start + 1; pos < len(text); {
		switch text[pos] {
		case '"':
			return pos + 1, nil
		case '\\':
			escapeStart := pos
			pos++
			if text[pos] != 'u' {
				pos++
				continue
			}
			value := jsonHexCodeUnit(text[pos+1 : pos+5])
			pos += 5
			switch {
			case value >= 0xD800 && value <= 0xDBFF:
				if pos+6 > len(text) || text[pos] != '\\' || text[pos+1] != 'u' {
					return 0, fmt.Errorf("unpaired high surrogate in JSON string at byte %d", escapeStart+1)
				}
				low := jsonHexCodeUnit(text[pos+2 : pos+6])
				if low < 0xDC00 || low > 0xDFFF {
					return 0, fmt.Errorf("unpaired high surrogate in JSON string at byte %d", escapeStart+1)
				}
				pos += 6
			case value >= 0xDC00 && value <= 0xDFFF:
				return 0, fmt.Errorf("unpaired low surrogate in JSON string at byte %d", escapeStart+1)
			}
		default:
			pos++
		}
	}
	return 0, fmt.Errorf("unterminated JSON string at byte %d", start+1)
}

// jsonHexCodeUnit receives exactly four valid hexadecimal digits because the
// complete document has passed json.Valid before scanning starts.
func jsonHexCodeUnit(digits string) uint16 {
	var value uint16
	for i := 0; i < len(digits); i++ {
		value <<= 4
		switch ch := digits[i]; {
		case ch >= '0' && ch <= '9':
			value |= uint16(ch - '0')
		case ch >= 'a' && ch <= 'f':
			value |= uint16(ch - 'a' + 10)
		default:
			value |= uint16(ch - 'A' + 10)
		}
	}
	return value
}

func jsonStringIsKey(text string, end int) bool {
	for end < len(text) {
		switch text[end] {
		case ' ', '\t', '\r', '\n':
			end++
		default:
			return text[end] == ':'
		}
	}
	return false
}

func encodeJSONString(value string) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", fmt.Errorf("encode cleaned JSON string: %w", err)
	}
	// Encoder.Encode always appends one line feed; it is not part of the token.
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}
