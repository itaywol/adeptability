package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// In --json mode the install result must be emitted as valid JSON (not the
// human "installed ..." line), so a caller can pipe it into a JSON parser.
func TestSkillInstallRenderable_JSONMode(t *testing.T) {
	d := &Deps{Flags: &GlobalFlags{JSON: true}}
	var buf bytes.Buffer
	r := &skillInstallRenderable{
		ID:        "myskill",
		Slug:      "owner/repo/myskill",
		SHA:       "deadbeefcafebabe",
		Path:      "myskill",
		Files:     []string{"SKILL.md"},
		ScanWorst: "none",
	}
	require.NoError(t, d.Print(&buf, r))

	var out map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out), "stdout must be valid JSON in --json mode")
	require.Equal(t, "myskill", out["id"])
	require.Equal(t, "owner/repo/myskill", out["slug"])
	require.NotContains(t, buf.String(), "installed ")
}

// In plain mode the legacy one-line summary is preserved.
func TestSkillInstallRenderable_PlainMode(t *testing.T) {
	d := &Deps{Flags: &GlobalFlags{}}
	var buf bytes.Buffer
	r := &skillInstallRenderable{ID: "myskill", Slug: "owner/repo/myskill", SHA: "deadbeefcafebabe"}
	require.NoError(t, d.Print(&buf, r))
	require.Contains(t, buf.String(), "installed myskill @ deadbeef (owner/repo/myskill)")
}

// writeExternalSkillAt must reject traversal/absolute keys even if a
// malicious tarball slipped one past the extractor: defense in depth so
// nothing is ever written outside the destination skill dir.
func TestWriteExternalSkillAt_RejectsTraversal(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "skills", "myskill")
	canary := filepath.Join(t.TempDir(), "canary")

	files := map[string][]byte{
		"SKILL.md":                    []byte("# ok"),
		"../../../../../../" + canary: []byte("pwned"),
	}
	err := writeExternalSkillAt(dst, files)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsafe skill file path")

	// The escaping write must not have happened.
	_, statErr := os.Stat(canary)
	require.True(t, os.IsNotExist(statErr), "canary file should not have been written")
}

// A bare ".." key must be rejected too.
func TestWriteExternalSkillAt_RejectsDotDot(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "skills", "myskill")
	err := writeExternalSkillAt(dst, map[string][]byte{"..": []byte("x")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsafe skill file path")
}

// An absolute key must be rejected.
func TestWriteExternalSkillAt_RejectsAbsolute(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "skills", "myskill")
	abs := filepath.Join(t.TempDir(), "abs-target")
	err := writeExternalSkillAt(dst, map[string][]byte{abs: []byte("x")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsafe skill file path")
	_, statErr := os.Stat(abs)
	require.True(t, os.IsNotExist(statErr))
}

// Happy path: legitimate nested relative keys are written under dst.
func TestWriteExternalSkillAt_HappyPath(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "skills", "myskill")
	files := map[string][]byte{
		"SKILL.md":          []byte("# ok"),
		"references/api.md": []byte("api"),
	}
	require.NoError(t, writeExternalSkillAt(dst, files))

	got, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("# ok"), got)

	got, err = os.ReadFile(filepath.Join(dst, "references", "api.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("api"), got)
}
