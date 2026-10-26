package cleaner

import (
	"io"

	"github.com/1mm0rt41PC/rmwm-remove-ai-watermarks/internal/textio"
)

// ************************************************************************************************
// Mock functions provide narrow seams for new API I/O and internal operations.
var (
	mock_ioReadAll     = io.ReadAll
	mk_cleanStream     = cleanStream
	mk_streamConfig    = streamConfig
	mk_nilInterface    = nilInterface
	mk_decodeText      = textio.DecodeText
	mk_decodeTextBytes = textio.DecodeTextBytes
	mk_writeAll        = textio.WriteAll
	mk_cleanDocument   = cleanDocument
	mock_clean         = textio.Clean[CleanOptions, CleanStats]
)
