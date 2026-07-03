package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/pkg/adept"
)

func runLoopCmd(t *testing.T, d *Deps, args ...string) (string, error) {
	t.Helper()
	cmd := newLoopCmd(d)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestLoopAdd_ComposesSkillAgentAndChecklist(t *testing.T) {
	root := t.TempDir()
	d := testDeps(t, root, t.TempDir())

	out, err := runLoopCmd(t, d, "add", "ci-triage", "--workflow")
	require.NoError(t, err)
	assert.Contains(t, out, "created discovery skill ci-triage")
	assert.Contains(t, out, "created evaluator agent ci-triage-reviewer")
	assert.Contains(t, out, "created schedule skeleton")
	assert.Contains(t, out, "first-loop checklist")
	assert.Contains(t, out, "state file")
	assert.Contains(t, out, "token cap")
	assert.Contains(t, out, "human review")

	p, err := d.Project()
	require.NoError(t, err)

	// Discovery skill: triage template, parses + validates, has the five
	// loop-discovery sections including Stop.
	require.True(t, p.HasSkill("ci-triage"))
	raw, err := os.ReadFile(filepath.Join(p.SkillsDir(), "ci-triage", adept.SkillFileName))
	require.NoError(t, err)
	skill, _, err := canonical.NewParser().ParseFrontmatter(raw)
	require.NoError(t, err)
	skill.ID = "ci-triage"
	v, err := canonical.NewValidator()
	require.NoError(t, err)
	require.NoError(t, v.Validate(skill))
	assert.Equal(t, adept.ActivationManual, skill.Activation, "discovery skill must not auto-fire")
	for _, section := range []string{"## Read", "## Judge", "## Write", "## Hand off", "## Stop"} {
		assert.Contains(t, string(raw), section)
	}
	assert.Contains(t, string(raw), "state/ci-triage.md")

	// Evaluator agent: evaluator template under the derived id.
	require.True(t, p.HasAgent("ci-triage-reviewer"))
	a, err := p.GetAgent("ci-triage-reviewer")
	require.NoError(t, err)
	assert.Contains(t, a.Body, "ASSUME the work is broken")

	// Workflow skeleton mentions the skill by name (discovery via skill,
	// not inline prompt) and persists state.
	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "adept-loop-ci-triage.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(wf), "cron:")
	assert.Contains(t, string(wf), `"ci-triage" skill`)
	assert.Contains(t, string(wf), "persist loop state")
}

func TestLoopAdd_Guards(t *testing.T) {
	root := t.TempDir()
	d := testDeps(t, root, t.TempDir())

	_, err := runLoopCmd(t, d, "add", "Bad_ID")
	require.Error(t, err)

	_, err = runLoopCmd(t, d, "add", "dup-loop")
	require.NoError(t, err)
	out, err := runLoopCmd(t, d, "add", "dup-loop")
	require.Error(t, err)
	assert.Contains(t, out+err.Error(), "already exists")

	// Existing workflow file is never overwritten.
	wfPath := filepath.Join(root, ".github", "workflows", "adept-loop-keep.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(wfPath), 0o755))
	require.NoError(t, os.WriteFile(wfPath, []byte("keep me\n"), 0o644))
	_, err = runLoopCmd(t, d, "add", "keep", "--workflow")
	require.ErrorContains(t, err, "refusing to overwrite")
	kept, err := os.ReadFile(wfPath)
	require.NoError(t, err)
	assert.Equal(t, "keep me\n", string(kept))
}

func TestSkillAdd_TriageTemplate(t *testing.T) {
	root := t.TempDir()
	d := testDeps(t, root, t.TempDir())

	cmd := newSkillCmd(d)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"add", "morning-triage", "--template", "triage"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "template: triage")

	p, err := d.Project()
	require.NoError(t, err)
	require.True(t, p.HasSkill("morning-triage"))

	cmd = newSkillCmd(d)
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"add", "x", "--template", "nonsense"})
	require.ErrorContains(t, cmd.Execute(), "unknown --template")
}
