package main

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanJSONPreservesUnchangedDocument(t *testing.T) {
	input := `{
  "z": 1.2300e+400,
  "a": "caf\u00e9 \/ <>&",
  "literal": "\\u200b",
  "emoji": "\ud83d\ude00",
  "nested": [true, null, -0.0, {"a": "ordinary"}]
}`
	input = " \r\n\t" + strings.ReplaceAll(input, "\n", "\r\n") + "\r\n\t"
	output, stats, err := CleanJSON(input, CleanOptions{NormalizeSpaces: true})
	if err != nil {
		t.Fatal(err)
	}
	if output != input {
		t.Fatalf("unchanged JSON was reformatted:\nwant %q\ngot  %q", input, output)
	}
	if stats.Removed != 0 || stats.Replaced != 0 {
		t.Fatalf("unexpected changes: %+v", stats)
	}
	if stats.InputRunes != utf8.RuneCountInString(input) || stats.OutputRunes != stats.InputRunes {
		t.Fatalf("incorrect document rune counts: %+v", stats)
	}
}

func TestCleanJSONCleansRawAndEscapedStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		removed  int
		replaced int
	}{
		{"escaped invisible", `{"x":"a\u200bb"}`, `{"x":"ab"}`, 1, 0},
		{"raw invisible", "{\"x\":\"a\u200bb\"}", `{"x":"ab"}`, 1, 0},
		{"escaped NBSP", `{"x":"a\u00a0b"}`, `{"x":"a b"}`, 0, 1},
		{"raw NBSP", "{\"x\":\"a\u00a0b\"}", `{"x":"a b"}`, 0, 1},
		{"raw dashes", `{"x":"a—b⸺c⸻"}`, `{"x":"a-b-c-"}`, 0, 3},
		{"escaped dashes", `{"x":"a\u2014b\u2e3Ac\u2E3b"}`, `{"x":"a-b-c-"}`, 0, 3},
		{"literal dash escapes", `{"x":"\\u2014 \\u2e3a \\u2e3b"}`, `{"x":"\\u2014 \\u2e3a \\u2e3b"}`, 0, 0},
		{"literal escape", `{"x":"\\u200b"}`, `{"x":"\\u200b"}`, 0, 0},
		{"literal escape inside changed token", `{"x":"a\\u200bb\u200b"}`, `{"x":"a\\u200bb"}`, 1, 0},
		{"keys and nested values", `["a\u200b",{"k\u200b":"\u00a0b"}]`, `["a",{"k":" b"}]`, 2, 1},
		{"root string", `"\u200b"`, `""`, 1, 0},
		{"HTML characters", `{"x":"<>&\u200b"}`, `{"x":"<>&"}`, 1, 0},
		{"punctuation inside string", `{"x":"{\"k\":\"v\"}\u200b"}`, `{"x":"{\"k\":\"v\"}"}`, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, stats, err := CleanJSON(tt.input, CleanOptions{NormalizeSpaces: true, NormalizeDashes: true})
			if err != nil {
				t.Fatal(err)
			}
			if output != tt.want {
				t.Fatalf("want %q; got %q", tt.want, output)
			}
			if !json.Valid([]byte(output)) {
				t.Fatalf("output is not valid JSON: %q", output)
			}
			if stats.Removed != tt.removed || stats.Replaced != tt.replaced {
				t.Fatalf("want removed=%d replaced=%d; got %+v", tt.removed, tt.replaced, stats)
			}
			if stats.InputRunes != utf8.RuneCountInString(tt.input) || stats.OutputRunes != utf8.RuneCountInString(output) {
				t.Fatalf("incorrect document rune counts: %+v", stats)
			}
		})
	}
}

func TestCleanJSONPreservesLayoutOrderAndNumberSpelling(t *testing.T) {
	input := " \t{\r\n  \"z\":1.2300e+400,\n  \"k\\u200b\" : \"a\\u200bb\",\r\n\t\"escaped\":\"\\u0061\\/\", \"n\":-0.00\r\n}\t\n"
	want := " \t{\r\n  \"z\":1.2300e+400,\n  \"k\" : \"ab\",\r\n\t\"escaped\":\"\\u0061\\/\", \"n\":-0.00\r\n}\t\n"
	output, _, err := CleanJSON(input, CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if output != want {
		t.Fatalf("bytes outside changed tokens differ:\nwant %q\ngot  %q", want, output)
	}
}

func TestCleanJSONPreservesEmojiAndSurrogatePairs(t *testing.T) {
	inputs := []string{
		`{"emoji":"\ud83d\ude00"}`,
		`{"emoji":"\uD83D\uDE00"}`,
		`{"emoji":"😀 👩‍💻 🏳️‍🌈"}`,
		`{"literal":"\\ud800"}`,
	}
	for _, input := range inputs {
		output, _, err := CleanJSON(input, CleanOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if output != input {
			t.Fatalf("unchanged token was altered: want %q; got %q", input, output)
		}
	}

	output, _, err := CleanJSON(`{"emoji":"\ud83d\ude00\u200b"}`, CleanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["emoji"] != "😀" {
		t.Fatalf("surrogate pair was not preserved semantically: %q", decoded["emoji"])
	}
}

func TestCleanJSONRejectsInvalidInputWithoutPartialResult(t *testing.T) {
	inputs := []string{
		"",
		`{"a":1,}`,
		`{"x":"unterminated}`,
		`{"x":"\uZZZZ"}`,
		"{\"x\":\"raw\nnewline\"}",
		`{"x":"\ud800"}`,
		`{"x":"\udfff"}`,
		`{"x":"\ud800\ud800"}`,
		`{"x":"\ud800x\udc00"}`,
		`{"x":"\ud800\\udc00"}`,
		`{"\ud800":"x"}`,
		`{"x":"\u200b","bad":"\ud800"}`,
		`{"a":1}` + "\n" + `{"b":2}`,
		string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}),
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			output, stats, err := CleanJSON(input, CleanOptions{})
			if err == nil {
				t.Fatalf("expected invalid input to fail: %q => %q", input, output)
			}
			if output != "" || stats != (CleanStats{}) {
				t.Fatalf("partial result returned on error: %q, %+v", output, stats)
			}
		})
	}
}

func TestCleanJSONRejectsDuplicateKeysAfterCleaning(t *testing.T) {
	inputs := []string{
		`{"a":1,"a":2}`,
		`{"a":1,"\u0061":2}`,
		`{"a\u200b":1,"a":2}`,
		`{"a":1,"a\u200b":2}`,
		`{"a b":1,"a\u00a0b":2}`,
		`{"a-b":1,"a\u2014b":2}`,
		`{"a⸺b":1,"a\u2e3bb":2}`,
		`{"nested":[{"a":1,"a\u200b":2}]}`,
		`{"\ud83d\ude00":1,"😀":2}`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			output, _, err := CleanJSON(input, CleanOptions{NormalizeSpaces: true, NormalizeDashes: true})
			if err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
				t.Fatalf("expected duplicate key error; got output=%q, err=%v", output, err)
			}
			if output != "" {
				t.Fatalf("partial JSON returned after key collision: %q", output)
			}
		})
	}
}

func TestCleanJSONAllowsSameKeysInDifferentObjects(t *testing.T) {
	inputs := []string{
		`{"a":1,"nested":{"a":2}}`,
		`[{"a":1},{"a":2}]`,
		`{"x":[{"a":1},{"a":2}],"a":3}`,
	}
	for _, input := range inputs {
		output, _, err := CleanJSON(input, CleanOptions{})
		if err != nil {
			t.Fatalf("separate object scopes should not collide: %v", err)
		}
		if output != input {
			t.Fatalf("want %q; got %q", input, output)
		}
	}
}

func TestCleanJSONOptionsApplyToEscapedCharacters(t *testing.T) {
	input := `{"a b":1,"a\u00a0b":2,"x":"a\u00a0b"}`
	output, stats, err := CleanJSON(input, CleanOptions{NormalizeSpaces: false})
	if err != nil {
		t.Fatal(err)
	}
	if output != input || stats.Replaced != 0 {
		t.Fatalf("NormalizeSpaces=false should preserve NBSP escapes: %q, %+v", output, stats)
	}

	input = `{"emoji":"\ud83d\udc69\u200d\ud83d\udcbb"}`
	output, _, err = CleanJSON(input, CleanOptions{StripEmojiGlue: true})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(decoded["emoji"], '\u200d') || decoded["emoji"] != "👩💻" {
		t.Fatalf("StripEmojiGlue=true was not applied to decoded escapes: %q", decoded["emoji"])
	}
}

func TestCleanJSONPreservesDashesWhenDisabled(t *testing.T) {
	input := `{"a-b":1,"a\u2014b":2,"x":"— \u2E3A \u2e3b"}`
	output, stats, err := CleanJSON(input, CleanOptions{NormalizeSpaces: true, NormalizeDashes: false})
	if err != nil || output != input || stats.Removed != 0 || stats.Replaced != 0 {
		t.Fatalf("disabled dash normalization changed JSON or caused a collision: output=%q stats=%+v err=%v", output, stats, err)
	}
}

func TestCleanJSONAcceptsPrimitiveDocuments(t *testing.T) {
	for _, input := range []string{"false", "null", " 3.00e+400\r\n", "[]", "{}", `"ordinary"`} {
		output, _, err := CleanJSON(input, CleanOptions{})
		if err != nil || output != input {
			t.Fatalf("primitive document %q => %q, %v", input, output, err)
		}
	}
}
