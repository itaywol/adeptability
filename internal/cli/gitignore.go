package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/itaywol/adeptability/internal/fsutil"
)

// scopeIgnoreLines are the .adeptability subpaths that are machine-local by
// default: library clones (re-hydratable from config.json) and render staging.
var scopeIgnoreLines = []string{"libs/", "staging/"}

// ensureScopeGitignore idempotently appends the scope-local ignore lines to
// <baseDir>/.gitignore, preserving any user content already there.
func ensureScopeGitignore(w fsutil.Writer, baseDir string) error {
	path := filepath.Join(baseDir, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	out := strings.TrimRight(string(existing), "\n")
	added := false
	for _, line := range scopeIgnoreLines {
		if !have[line] {
			if out != "" {
				out += "\n"
			}
			out += line
			added = true
		}
	}
	if !added {
		return nil
	}
	return w.AtomicWrite(path, []byte(out+"\n"), 0o644)
}
