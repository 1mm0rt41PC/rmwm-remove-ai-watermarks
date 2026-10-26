package cleaner

// ************************************************************************************************
// cleanSettings contains only document-cleaning options, independent of any CLI.
type cleanSettings struct {
	CleanOptions
	format string
}

// ************************************************************************************************
// cleanDocument applies the public Unicode/JSON rules with shared encoding safety.
func cleanDocument(data []byte, name string, cfg cleanSettings) ([]byte, CleanStats, error) {
	return mock_clean(data, name, cfg.format, cfg.CleanOptions, CleanText, CleanJSON)
}
