package textio

// ************************************************************************************************
// Mock functions isolate decoding and input validation for tests.
var (
	mk_decodeTextBytes = DecodeTextBytes
	mk_validateText    = ValidateText
	mk_decodeText      = DecodeText
	mk_encodeText      = EncodeText
)
