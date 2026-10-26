package cleaner

import (
	"fmt"
	"runtime"
)

// ************************************************************************************************
// testId identifies a test case by the source line declaring it.
func testId(message string) string {
	if message == "" {
		return ""
	}
	_, _, line, ok := runtime.Caller(1)
	if !ok {
		return message
	}
	return fmt.Sprintf("T%d: %s", line, message)
}
