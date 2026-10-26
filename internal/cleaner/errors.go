package cleaner

import "errors"

// ************************************************************************************************
// API sentinel errors support errors.Is without external dependencies.
var (
	ErrNilArgument   = errors.New("nil reader or writer")
	ErrInvalidFormat = errors.New("format must be auto, text or json")
)
