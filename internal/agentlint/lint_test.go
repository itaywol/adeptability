package agentlint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/scan"
	"github.com/itaywol/adeptability/pkg/adept"
)

// goodAgent passes every rule: descriptive trigger, structured body with
// boundaries, restricted tools, no model.
func goodAgent() *adept.Agent {
	return &adept.Agent{
		ID:          "go-test-runner",
		Description: "Runs the Go test suite and reports failures. Use proactively after editing any *.go file.",
		Mode:        adept.AgentModeSubagent,
		Tools:       []string{"Read", "Grep", "Bash"},
		Body: `You are a Go test runner.

## When invoked

1. Run the tests.
2. Summarize failures.

## Boundaries

- Do not modify source files.
`,
	}
}

func hasRule(findings []scan.Finding, id string) bool {
	for _, f := range findings {
		if f.ID == id {
			return true
		}
	}
	return false
}

func TestLint_CleanAgent(t *testing.T) {
	t.Parallel()
	findings := NewLinter().Lint(Input{Agent: goodAgent()})
	assert.Empty(t, findings, "expected no findings, got %+v", findings)
}

func TestLint_Rules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(a *adept.Agent)
		want   string
		sev    scan.Severity
	}{
		{"missing description", func(a *adept.Agent) { a.Description = "" }, "AGENT-LINT-001", scan.SeverityHigh},
		{"bad id", func(a *adept.Agent) { a.ID = "Bad_ID" }, "AGENT-LINT-002", scan.SeverityHigh},
		{"empty body", func(a *adept.Agent) { a.Body = "" }, "AGENT-LINT-003", scan.SeverityHigh},
		{"opencode model grammar", func(a *adept.Agent) { a.Model = "sonnet"; a.Targets = []string{"opencode"} }, "AGENT-LINT-004", scan.SeverityHigh},
		{"vague description", func(a *adept.Agent) { a.Description = "A helper." }, "AGENT-LINT-101", scan.SeverityMedium},
		{"body too short", func(a *adept.Agent) { a.Body = "Do stuff." }, "AGENT-LINT-102", scan.SeverityMedium},
		{"readonly mismatch", func(a *adept.Agent) {
			a.Tools = []string{"Read", "Edit"}
			a.Body = "Never modify any file.\n\n## Steps\n\n1. Look.\n"
		}, "AGENT-LINT-103", scan.SeverityMedium},
		{"unknown claude tool", func(a *adept.Agent) { a.Targets = []string{"claude-code"}; a.Tools = []string{"Read", "Hammer"} }, "AGENT-LINT-104", scan.SeverityMedium},
		{"multi-harness model", func(a *adept.Agent) { a.Model = "sonnet" }, "AGENT-LINT-106", scan.SeverityMedium},
		{"no proactive hint", func(a *adept.Agent) {
			a.Description = "Runs tests when the suite changes."
		}, "AGENT-LINT-201", scan.SeverityLow},
		{"missing structure", func(a *adept.Agent) {
			a.Body = "You run tests and never break things. Report results faithfully and be thorough about failure output."
		}, "AGENT-LINT-202", scan.SeverityLow},
		{"missing boundaries", func(a *adept.Agent) {
			a.Tools = nil // inherits everything -> write capable
			a.Body = "You are a fixer.\n\n## When invoked\n\n1. Fix the bug.\n2. Run tests.\n"
		}, "AGENT-LINT-204", scan.SeverityLow},
		{"evaluator cannot act", func(a *adept.Agent) {
			a.ID = "code-reviewer"
			a.Tools = []string{"Read", "Grep"}
		}, "AGENT-LINT-205", scan.SeverityLow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := goodAgent()
			tt.mutate(a)
			findings := NewLinter().Lint(Input{Agent: a})
			require.True(t, hasRule(findings, tt.want), "expected %s in %+v", tt.want, findings)
			for _, f := range findings {
				if f.ID == tt.want {
					assert.Equal(t, tt.sev, f.Severity)
				}
			}
		})
	}
}

func TestLint_OverlappingDescriptions(t *testing.T) {
	t.Parallel()
	a := goodAgent()
	b := goodAgent()
	b.ID = "go-test-runner-two"
	findings := NewLinter().Lint(Input{Agent: a, AllAgents: []*adept.Agent{a, b}})
	require.True(t, hasRule(findings, "AGENT-LINT-105"), "got %+v", findings)
}

func TestLint_OverlongPrompt(t *testing.T) {
	t.Parallel()
	a := goodAgent()
	for range 600 {
		a.Body += "More instructions here.\n"
	}
	findings := NewLinter().Lint(Input{Agent: a})
	assert.True(t, hasRule(findings, "AGENT-LINT-203"))
}

func TestLint_NilAgent(t *testing.T) {
	t.Parallel()
	assert.Nil(t, NewLinter().Lint(Input{}))
}

func TestExtractFileRefs(t *testing.T) {
	t.Parallel()
	body := "Read `./scripts/run.sh` first, then [notes](./docs/notes.md).\n" +
		"Mention of `internal/canonical/parser.go` and @refs/data.json here.\n" +
		"Not files: user@example.com, https://example.com/x.md, `*.go`, </placeholder/x.md>, `/abs/path.md`.\n" +
		"```\n`./inside/fence.md`\n```\n"
	refs := ExtractFileRefs(body)
	byPath := map[string]bool{}
	explicit := map[string]bool{}
	for _, r := range refs {
		byPath[r.Path] = true
		explicit[r.Path] = r.Explicit
	}
	assert.True(t, byPath["./scripts/run.sh"])
	assert.True(t, explicit["./scripts/run.sh"])
	assert.True(t, byPath["./docs/notes.md"])
	assert.True(t, byPath["internal/canonical/parser.go"])
	assert.False(t, explicit["internal/canonical/parser.go"])
	assert.True(t, byPath["refs/data.json"])
	assert.False(t, byPath["example.com"])
	assert.False(t, byPath["./inside/fence.md"], "fenced refs must not fire")
	for p := range byPath {
		assert.NotContains(t, p, "example.com")
		assert.NotContains(t, p, "abs")
	}
}

func TestRuleFileReferences(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755))

	a := goodAgent()
	a.Body += "\nRun `./scripts/run.sh` then read `./missing/file.md` and @gone/thing.json.\n"
	findings := NewLinter().Lint(Input{Agent: a, ProjectRoot: root})

	var explicitMiss, bareMiss, existingHit bool
	for _, f := range findings {
		switch {
		case f.ID == "AGENT-LINT-005" && f.Evidence == "./missing/file.md":
			explicitMiss = true
			assert.Equal(t, scan.SeverityHigh, f.Severity)
		case f.ID == "AGENT-LINT-206" && f.Evidence == "gone/thing.json":
			bareMiss = true
			assert.Equal(t, scan.SeverityLow, f.Severity)
		case f.Evidence == "./scripts/run.sh":
			existingHit = true
		}
	}
	assert.True(t, explicitMiss, "explicit missing ref must be an error: %+v", findings)
	assert.True(t, bareMiss, "bare missing ref must be info: %+v", findings)
	assert.False(t, existingHit, "existing file must not fire")
}
