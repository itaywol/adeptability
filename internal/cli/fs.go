package cli

import "os"

// readFile is a tiny wrapper kept private to the cli package so callers don't
// need to import os in every command file.
func readFile(path string) ([]byte, error) { return os.ReadFile(path) }
