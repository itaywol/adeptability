// Package clitest provides integration helpers for command-level tests.
//
// Tests build the adept binary once via go test main, then exercise it as a
// subprocess in a t.TempDir() sandbox. Keeps integration tests honest:
// real binary, real filesystem, real exit codes.
package clitest

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Runner runs the adept binary from a fixed path against a fresh sandbox.
type Runner struct {
	BinPath string
	LibDir  string
	WorkDir string
	Env     []string
	Timeout time.Duration
}

// Result captures one CLI invocation.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// New constructs a Runner. binPath should be an already-built adept binary
// (use BuildBinary in TestMain).
func New(t *testing.T, binPath string) *Runner {
	t.Helper()
	lib := filepath.Join(t.TempDir(), "library")
	work := filepath.Join(t.TempDir(), "project")
	return &Runner{
		BinPath: binPath,
		LibDir:  lib,
		WorkDir: work,
		Env:     []string{"ADEPT_LIBRARY=" + lib, "HOME=" + t.TempDir()},
		Timeout: 30 * time.Second,
	}
}

// Run executes adept with the given args inside the sandbox.
func (r *Runner) Run(args ...string) Result {
	ctx, cancel := context.WithTimeout(context.Background(), r.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.BinPath, args...)
	cmd.Dir = r.WorkDir
	cmd.Env = append([]string{}, r.Env...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errBuf.String(), Err: err}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		res.Err = nil
	}
	return res
}

// BuildBinary compiles the adept binary into a temp file and returns its path.
// Call once from TestMain to avoid rebuilding per test.
func BuildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "adept-test")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/adept")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build adept: %v\n%s", err, out)
	}
	return bin
}

// repoRoot walks up from the current package to find the repo root (where go.mod sits).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	return string(bytes.TrimSpace(wd))
}
