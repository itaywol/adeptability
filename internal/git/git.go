// Package git wraps the git CLI behind two small interfaces: Runner (executes
// arbitrary git commands) and Client (high-level operations the rest of the
// codebase actually needs).
//
// Both interfaces accept context for cancellation. Callers that need to test
// git interactions should inject a mock Runner rather than shelling out.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Result is the captured output of a single git invocation.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Runner executes a git command with the given working directory and args.
//
// Implementations MUST NOT panic on non-zero exit codes; they should populate
// Result and return a non-nil error so callers can distinguish process
// failures (e.g. "git not installed") from command failures (e.g. "fatal:
// not a git repository").
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (Result, error)
}

// Client is the high-level surface used by callers.
type Client interface {
	IsRepo(dir string) bool
	Init(ctx context.Context, dir string) error
	Add(ctx context.Context, dir, path string) error
	Commit(ctx context.Context, dir, msg string) (hash string, err error)
	Status(ctx context.Context, dir string) (dirty bool, lines []string, err error)
	HeadHash(ctx context.Context, dir string) (string, error)
	EnsureConfig(ctx context.Context, dir, key, value string) error
	// CloneOrPull clones url@ref into dest if dest is not already a git
	// repository, otherwise runs `git fetch && git checkout ref` in place.
	// Used by `adept init --from <git-url>` to bootstrap a library remote.
	CloneOrPull(ctx context.Context, url, ref, dest string) error
}

// execRunner shells out to the configured git binary.
type execRunner struct {
	bin string
}

// NewExecRunner returns a Runner backed by os/exec. Pass "" to default to
// "git" on $PATH.
func NewExecRunner(gitBin string) Runner {
	if gitBin == "" {
		gitBin = "git"
	}
	return &execRunner{bin: gitBin}
}

func (r *execRunner) Run(ctx context.Context, dir string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, r.bin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Command ran, returned non-zero. Surface that as an error so
			// callers can decide, but with res populated.
			return res, fmt.Errorf("git %s: exit %d: %s",
				strings.Join(args, " "), exitErr.ExitCode(), strings.TrimSpace(res.Stderr))
		}
		return res, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return res, nil
}

// client wraps Runner with parsed semantics for the common operations.
type client struct {
	r Runner
}

// NewClient returns a Client backed by r. r must not be nil.
func NewClient(r Runner) Client {
	if r == nil {
		r = NewExecRunner("")
	}
	return &client{r: r}
}

func (c *client) IsRepo(dir string) bool {
	// Cheap heuristic: <dir>/.git exists as a file or directory.
	st, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false
		}
		return false
	}
	_ = st
	return true
}

func (c *client) Init(ctx context.Context, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("git: ensure dir %s: %w", dir, err)
	}
	if _, err := c.r.Run(ctx, dir, "init"); err != nil {
		return fmt.Errorf("git init %s: %w", dir, err)
	}
	return nil
}

func (c *client) Add(ctx context.Context, dir, path string) error {
	if _, err := c.r.Run(ctx, dir, "add", path); err != nil {
		return fmt.Errorf("git add %s: %w", path, err)
	}
	return nil
}

// Commit creates a commit with msg and returns the resulting HEAD hash.
// If there is nothing to commit, returns an empty string and a wrapped error.
func (c *client) Commit(ctx context.Context, dir, msg string) (string, error) {
	if _, err := c.r.Run(ctx, dir, "commit", "-m", msg); err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}
	return c.HeadHash(ctx, dir)
}

func (c *client) Status(ctx context.Context, dir string) (bool, []string, error) {
	res, err := c.r.Run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, nil, fmt.Errorf("git status: %w", err)
	}
	out := strings.TrimRight(res.Stdout, "\n")
	if out == "" {
		return false, nil, nil
	}
	lines := strings.Split(out, "\n")
	return true, lines, nil
}

func (c *client) HeadHash(ctx context.Context, dir string) (string, error) {
	res, err := c.r.Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(res.Stdout), nil
}

// CloneOrPull clones url@ref into dest, or fetches+checks-out ref if dest
// is already a git working tree. The empty string for ref means HEAD/the
// remote's default branch.
func (c *client) CloneOrPull(ctx context.Context, url, ref, dest string) error {
	if dest == "" {
		return fmt.Errorf("git clone: empty destination")
	}
	if c.IsRepo(dest) {
		if _, err := c.r.Run(ctx, dest, "fetch", "--all", "--prune"); err != nil {
			return fmt.Errorf("git fetch in %s: %w", dest, err)
		}
		if ref != "" {
			if _, err := c.r.Run(ctx, dest, "checkout", ref); err != nil {
				return fmt.Errorf("git checkout %s in %s: %w", ref, dest, err)
			}
			if _, err := c.r.Run(ctx, dest, "pull", "--ff-only", "origin", ref); err != nil {
				return fmt.Errorf("git pull %s in %s: %w", ref, dest, err)
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("git clone: ensure parent dir %s: %w", dest, err)
	}
	args := []string{"clone"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, dest)
	if _, err := c.r.Run(ctx, "", args...); err != nil {
		return fmt.Errorf("git clone %s into %s: %w", url, dest, err)
	}
	return nil
}

// EnsureConfig sets key=value when the current value differs (or is missing).
func (c *client) EnsureConfig(ctx context.Context, dir, key, value string) error {
	got, err := c.r.Run(ctx, dir, "config", "--get", key)
	if err == nil && strings.TrimSpace(got.Stdout) == value {
		return nil
	}
	if _, setErr := c.r.Run(ctx, dir, "config", key, value); setErr != nil {
		return fmt.Errorf("git config %s=%s: %w", key, value, setErr)
	}
	return nil
}
