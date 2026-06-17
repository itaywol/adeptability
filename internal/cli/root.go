// Package cli is the cobra-based command surface for adept.
//
// Composition root pattern: NewRoot wires every concrete implementation
// behind interfaces into a *Deps container, then attaches each subcommand
// constructed from that container. No package-level state. No init() side
// effects. Every command receives its dependencies explicitly so it can be
// tested with mocks.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/pkg/adept"
)

// BuildInfo is populated by main from -ldflags.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// GlobalFlags hold flags shared by every subcommand.
type GlobalFlags struct {
	JSON       bool
	LogLevel   string
	ProjectDir string
	LibraryDir string
}

// NewRoot builds the cobra root command with all subcommands attached.
func NewRoot(b BuildInfo) *cobra.Command {
	gf := &GlobalFlags{}
	root := &cobra.Command{
		Use:           "adept",
		Short:         "Cross-harness AI skill portability",
		Long:          "adept manages canonical AI assistant skills and renders them accurately into every harness in your project.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       fmt.Sprintf("%s (commit %s, built %s)", b.Version, b.Commit, b.Date),
		// Cobra's default "no subcommand matched" behavior prints help and
		// exits 0, which swallows typos like `adept totaly-fke`. RunE lets us
		// take over: bare `adept` still shows help (and exits 0), but any
		// stray positional becomes a hard "unknown command" error.
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return c.Help()
			}
			_ = c.Help()
			fmt.Fprintln(c.ErrOrStderr())
			return fmt.Errorf("unknown command %q for %q", args[0], c.CommandPath())
		},
	}

	root.PersistentFlags().BoolVar(&gf.JSON, "json", false, "emit machine-readable JSON output")
	root.PersistentFlags().StringVar(&gf.LogLevel, "log-level", "info", "log level: debug|info|warn|error")
	root.PersistentFlags().StringVar(&gf.ProjectDir, "project", "", "project root (default: current directory)")
	root.PersistentFlags().StringVar(&gf.LibraryDir, "library", "", "library root (default: $ADEPT_LIBRARY or $HOME/.adeptability)")

	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		// Resolve defaults at the latest possible moment so tests can override.
		if gf.ProjectDir == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolve project dir: %w", err)
			}
			gf.ProjectDir = cwd
		}
		switch gf.LogLevel {
		case "debug", "info", "warn", "error":
		default:
			return fmt.Errorf("invalid --log-level %q (want debug|info|warn|error)", gf.LogLevel)
		}
		return nil
	}

	deps, err := NewDeps(gf, b)
	if err != nil {
		// Surface dependency-wiring errors at command construction time;
		// cobra will print them on the first invocation.
		root.RunE = func(*cobra.Command, []string) error { return err }
		return root
	}

	root.AddCommand(
		newInitCmd(deps),
		newStatusCmd(deps),
		newSyncCmd(deps),
		newSyncFromCmd(deps),
		newDiffCmd(deps),
		newHarnessCmd(deps),
		newSkillCmd(deps),
		newLibraryCmd(deps),
		newConfigCmd(deps),
	)
	applyUsageOnArgError(root)
	return root
}

// applyUsageOnArgError walks the command tree and makes sure that
// argument-shape or flag-shape errors (missing positional, missing required
// flag value, unknown flag, unknown subcommand) print the command's help
// before surfacing the error. Runtime errors keep the lean "error: …" path
// because root.SilenceUsage stays true.
//
// Cobra has no single hook for "Args validator failed", so we wrap each
// command's Args function. Flag errors are handled by SetFlagErrorFunc.
func applyUsageOnArgError(cmd *cobra.Command) {
	original := cmd.Args
	cmd.Args = func(c *cobra.Command, args []string) error {
		if original != nil {
			if err := original(c, args); err != nil {
				_ = c.Help()
				fmt.Fprintln(c.ErrOrStderr())
				return err
			}
		}
		return nil
	}
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		_ = c.Help()
		fmt.Fprintln(c.ErrOrStderr())
		return err
	})
	for _, sub := range cmd.Commands() {
		applyUsageOnArgError(sub)
	}
}

// ExitFromError maps an error to an exit code.
//   - nil                  -> 0
//   - ErrDirty             -> 2 (drift / dirty state, used by doctor/status)
//   - ErrMergeConflict     -> 2 (resolve --strategy merge surfaced conflicts)
//   - any other err        -> 1
func ExitFromError(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, ErrDirty) {
		return 2
	}
	if errors.Is(err, adept.ErrMergeConflict) {
		return 2
	}
	return 1
}

// ErrDirty is the sentinel returned by commands that report drift but did not fail.
var ErrDirty = errors.New("dirty state detected")

// Surface for testing: re-export adept sentinels under the CLI package so
// command tests can `require.ErrorIs(err, cli.ErrSkillNotFound)` without an
// extra import.
var (
	ErrSkillNotFound      = adept.ErrSkillNotFound
	ErrSkillInvalid       = adept.ErrSkillInvalid
	ErrLockSchemaMismatch = adept.ErrLockSchemaMismatch
	ErrBudgetOverflow     = adept.ErrBudgetOverflow
	ErrAdapterInvalid     = adept.ErrAdapterInvalid
	ErrHarnessUnknown     = adept.ErrHarnessUnknown
	ErrMergeConflict      = adept.ErrMergeConflict
	ErrMergeBaseMissing   = adept.ErrMergeBaseMissing
)
