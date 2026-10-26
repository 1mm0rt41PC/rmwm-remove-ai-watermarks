package main

import (
	rmwm "github.com/1mm0rt41PC/rmwm-remove-ai-watermarks"
	"github.com/1mm0rt41PC/rmwm-remove-ai-watermarks/internal/textio"
)

// ************************************************************************************************
// Mock functions isolate the shared encoded-document cleaning boundary.
var mock_clean = textio.Clean[rmwm.CleanOptions, rmwm.CleanStats]
