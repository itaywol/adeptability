// Command adept is the adeptability CLI.
//
// Cross-harness AI skill portability: author skills once, render accurately
// for every supported harness (Claude Code, Cursor, Codex, Copilot, OpenCode,
// and any config-driven adapter you register).
package main

import (
	"fmt"
	"os"

	"github.com/itaywol/adeptability/internal/cli"
)

// Build metadata, injected by goreleaser via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	root := cli.NewRoot(cli.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(cli.ExitFromError(err))
	}
}
