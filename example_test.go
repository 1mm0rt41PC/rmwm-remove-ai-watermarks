package rmwm_test

import (
	"bytes"
	"fmt"
	"strings"

	rmwm "github.com/1mm0rt41PC/rmwm-remove-ai-watermarks"
)

// ************************************************************************************************
// ExampleCleanupWatermarks demonstrates importing the root library without a CLI.
func ExampleCleanupWatermarks() {
	var output bytes.Buffer
	err := rmwm.CleanupWatermarks(strings.NewReader("a\u200bb—c"), &output, rmwm.Opt{NormalizeDashes: true})
	fmt.Println(output.String(), err)
	// Output: ab-c <nil>
}

// ************************************************************************************************
// ExampleRunDry shows aligned changed lines in a serialized JSON document.
func ExampleRunDry() {
	parts, err := rmwm.RunDry(strings.NewReader("{\n  \"text\": \"a\\u200bb\"\n}"), rmwm.Opt{Format: "json"})
	if err != nil {
		panic(err)
	}
	fmt.Println(parts[0])
	// Output:
	//   "text": "a\u200bb"
	// ---
	//   "text": "ab"
}
