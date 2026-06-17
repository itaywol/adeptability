package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockRunner records invocations and returns canned responses.
type mockRunner struct {
	calls    [][]string
	dirs     []string
	results  map[string]Result // key = strings.Join(args, " ")
	errs     map[string]error
	fallback func(args []string) (Result, error)
}

func (m *mockRunner) Run(_ context.Context, dir string, args ...string) (Result, error) {
	m.calls = append(m.calls, append([]string(nil), args...))
	m.dirs = append(m.dirs, dir)
	key := strings.Join(args, " ")
	if r, ok := m.results[key]; ok {
		if err, ok := m.errs[key]; ok {
			return r, err
		}
		return r, nil
	}
	if m.fallback != nil {
		return m.fallback(args)
	}
	return Result{}, nil
}

func TestClient_IsRepo_DotGit(t *testing.T) {
	c := NewClient(&mockRunner{})
	dir := t.TempDir()
	require.False(t, c.IsRepo(dir))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	require.True(t, c.IsRepo(dir))
}

func TestClient_Init_CallsGitInit(t *testing.T) {
	m := &mockRunner{}
	c := NewClient(m)
	dir := t.TempDir()
	require.NoError(t, c.Init(context.Background(), dir))
	require.Len(t, m.calls, 1)
	require.Equal(t, []string{"init"}, m.calls[0])
	require.Equal(t, dir, m.dirs[0])
}

func TestClient_Add(t *testing.T) {
	m := &mockRunner{}
	c := NewClient(m)
	require.NoError(t, c.Add(context.Background(), "/repo", "skills/foo"))
	require.Len(t, m.calls, 1)
	require.Equal(t, []string{"add", "skills/foo"}, m.calls[0])
}

func TestClient_Commit_ReturnsHeadHash(t *testing.T) {
	m := &mockRunner{
		results: map[string]Result{
			"commit -m feat: add foo": {Stdout: ""},
			"rev-parse HEAD":          {Stdout: "abcdef123\n"},
		},
	}
	c := NewClient(m)
	hash, err := c.Commit(context.Background(), "/repo", "feat: add foo")
	require.NoError(t, err)
	require.Equal(t, "abcdef123", hash)
}

func TestClient_Commit_Failure(t *testing.T) {
	failErr := errors.New("nothing to commit")
	m := &mockRunner{
		results: map[string]Result{
			"commit -m msg": {ExitCode: 1, Stderr: failErr.Error()},
		},
		errs: map[string]error{
			"commit -m msg": failErr,
		},
	}
	c := NewClient(m)
	_, err := c.Commit(context.Background(), "/repo", "msg")
	require.Error(t, err)
}

func TestClient_Status_Clean(t *testing.T) {
	m := &mockRunner{results: map[string]Result{
		"status --porcelain": {Stdout: ""},
	}}
	c := NewClient(m)
	dirty, lines, err := c.Status(context.Background(), "/repo")
	require.NoError(t, err)
	require.False(t, dirty)
	require.Empty(t, lines)
}

func TestClient_Status_Dirty(t *testing.T) {
	m := &mockRunner{results: map[string]Result{
		"status --porcelain": {Stdout: " M file1\n?? new.txt\n"},
	}}
	c := NewClient(m)
	dirty, lines, err := c.Status(context.Background(), "/repo")
	require.NoError(t, err)
	require.True(t, dirty)
	require.Equal(t, []string{" M file1", "?? new.txt"}, lines)
}

func TestClient_StagedFiles(t *testing.T) {
	m := &mockRunner{results: map[string]Result{
		"diff --cached --name-only": {Stdout: ".claude/skills/a/SKILL.md\n.adeptability/skills/a/SKILL.md\n"},
	}}
	c := NewClient(m)
	files, err := c.StagedFiles(context.Background(), "/repo")
	require.NoError(t, err)
	require.Equal(t, []string{".claude/skills/a/SKILL.md", ".adeptability/skills/a/SKILL.md"}, files)
}

func TestClient_StagedFiles_Empty(t *testing.T) {
	m := &mockRunner{results: map[string]Result{
		"diff --cached --name-only": {Stdout: ""},
	}}
	c := NewClient(m)
	files, err := c.StagedFiles(context.Background(), "/repo")
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestClient_HeadHash(t *testing.T) {
	m := &mockRunner{results: map[string]Result{
		"rev-parse HEAD": {Stdout: "deadbeef\n"},
	}}
	c := NewClient(m)
	hash, err := c.HeadHash(context.Background(), "/repo")
	require.NoError(t, err)
	require.Equal(t, "deadbeef", hash)
}

func TestClient_EnsureConfig_AlreadySet(t *testing.T) {
	m := &mockRunner{results: map[string]Result{
		"config --get user.email": {Stdout: "me@example.com\n"},
	}}
	c := NewClient(m)
	require.NoError(t, c.EnsureConfig(context.Background(), "/repo", "user.email", "me@example.com"))
	require.Len(t, m.calls, 1, "should not call set if value matches")
}

func TestClient_EnsureConfig_SetsWhenMismatched(t *testing.T) {
	m := &mockRunner{results: map[string]Result{
		"config --get user.email": {Stdout: "other@example.com\n"},
	}}
	c := NewClient(m)
	require.NoError(t, c.EnsureConfig(context.Background(), "/repo", "user.email", "me@example.com"))
	require.Len(t, m.calls, 2)
	require.Equal(t, []string{"config", "user.email", "me@example.com"}, m.calls[1])
}

func TestClient_EnsureConfig_SetsWhenMissing(t *testing.T) {
	m := &mockRunner{
		results: map[string]Result{
			"config --get user.email": {ExitCode: 1},
		},
		errs: map[string]error{
			"config --get user.email": errors.New("not set"),
		},
	}
	c := NewClient(m)
	require.NoError(t, c.EnsureConfig(context.Background(), "/repo", "user.email", "me@example.com"))
	require.Len(t, m.calls, 2)
}

func TestClient_NilRunnerDefaults(t *testing.T) {
	// Compile-time: NewClient(nil) should still work — fallback to default exec runner.
	c := NewClient(nil)
	require.NotNil(t, c)
}

func TestExecRunner_BinaryMissing(t *testing.T) {
	r := NewExecRunner("absolutely-not-a-real-binary-xyz123")
	_, err := r.Run(context.Background(), "", "status")
	require.Error(t, err)
}

func TestExecRunner_NonZeroExit(t *testing.T) {
	// `false` returns exit code 1.
	r := NewExecRunner("false")
	res, err := r.Run(context.Background(), "", "anything")
	require.Error(t, err)
	require.Equal(t, 1, res.ExitCode)
}

func TestExecRunner_SuccessCapturesStdout(t *testing.T) {
	// `echo` writes to stdout and exits 0.
	r := NewExecRunner("echo")
	res, err := r.Run(context.Background(), "", "hello")
	require.NoError(t, err)
	require.Contains(t, res.Stdout, "hello")
}
