package latex

// ExtractMitex exposes extractMitex to the tests.
func ExtractMitex() (dir string, cleanup func() error, err error) { return extractMitex() }
