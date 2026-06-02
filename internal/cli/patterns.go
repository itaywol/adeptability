package cli

import "regexp"

// skillIDPattern mirrors the canonical schema's id pattern. Centralized
// here so commands can validate user input before reaching the writer.
var skillIDPattern = regexp.MustCompile(`^[a-z0-9_][a-z0-9_-]{0,49}$`)

// libraryNamePattern matches the same shape — short, kebab-case,
// filesystem-safe. Each named library becomes a subdirectory under
// $ADEPT_LIBRARY/libs/<name>/.
var libraryNamePattern = regexp.MustCompile(`^[a-z0-9_][a-z0-9_-]{0,49}$`)
