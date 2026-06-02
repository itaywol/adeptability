package perskill_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/internal/render/perskill"
	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

type fakeWriter struct{}

func (fakeWriter) AtomicWrite(_ string, _ []byte, _ fs.FileMode) error { return nil }
func (fakeWriter) EnsureDir(_ string) error                            { return nil }

type osLinker struct{}

func (osLinker) SymlinkOrCopy(target, linkPath string, _ bool) (adept.HarnessMode, error) {
	return adept.ModeSymlink, os.Symlink(target, linkPath)
}
func (osLinker) ReadSymlink(p string) (string, error) { return os.Readlink(p) }
func (osLinker) PathType(p string) fsutil.PathType {
	info, err := os.Lstat(p)
	if err != nil {
		return common.PathMissing
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return common.PathSymlink
	case info.IsDir():
		return common.PathDirectory
	default:
		return common.PathFile
	}
}

func junieSpec() adept.HarnessSpec {
	return adept.HarnessSpec{
		ID:         "junie",
		Name:       "Junie",
		Kind:       adept.KindPerSkill,
		OutputPath: ".junie/skills/{id}/SKILL.md",
		BaseDir:    ".junie",
		NeedsDir:   true,
	}
}

func TestRenderer_Render_BasicSkill(t *testing.T) {
	t.Parallel()
	r := perskill.NewRenderer(junieSpec())
	out, err := r.Render(context.Background(), adept.RenderInput{
		Skill: &adept.Skill{
			ID:          "hello",
			Description: "demo skill",
			Body:        "do the thing",
		},
		Harness: junieSpec(),
	})
	require.NoError(t, err)
	require.Equal(t, ".junie/skills/hello/SKILL.md", out.Path)
	require.Equal(t, "hello", out.SkillID)
	require.Contains(t, string(out.Bytes), "name: hello")
	require.Contains(t, string(out.Bytes), "description: demo skill")
	require.Contains(t, string(out.Bytes), "do the thing")
}

func TestRenderer_AllowedToolsAndManual(t *testing.T) {
	t.Parallel()
	r := perskill.NewRenderer(junieSpec())
	out, err := r.Render(context.Background(), adept.RenderInput{
		Skill: &adept.Skill{
			ID:           "guarded",
			Description:  "x",
			AllowedTools: []string{"Read", "Grep"},
			Activation:   adept.ActivationManual,
			Body:         "b",
		},
		Harness: junieSpec(),
	})
	require.NoError(t, err)
	require.Contains(t, string(out.Bytes), "allowed-tools: [Read, Grep]")
	require.Contains(t, string(out.Bytes), "disable-model-invocation: true")
}

func TestRenderer_GlobsEmbedHint(t *testing.T) {
	t.Parallel()
	r := perskill.NewRenderer(junieSpec())
	out, err := r.Render(context.Background(), adept.RenderInput{
		Skill: &adept.Skill{
			ID:          "tsfiles",
			Description: "ts only",
			Activation:  adept.ActivationGlobs,
			Globs:       []string{"**/*.ts"},
			Body:        "b",
		},
		Harness: junieSpec(),
	})
	require.NoError(t, err)
	require.Contains(t, string(out.Bytes), "matches: **/*.ts")
}

func TestRenderer_Errors(t *testing.T) {
	t.Parallel()
	r := perskill.NewRenderer(junieSpec())
	_, err := r.Render(context.Background(), adept.RenderInput{Skill: nil})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
	_, err = r.Render(context.Background(), adept.RenderInput{Skill: &adept.Skill{}})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
	_, err = r.Render(context.Background(), adept.RenderInput{Skill: &adept.Skill{ID: "x"}})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestAdapter_DetectBaseDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := perskill.NewAdapter(junieSpec(), fakeWriter{}, osLinker{})
	ok, _ := a.Detect(root)
	require.False(t, ok)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".junie"), 0o755))
	ok, err := a.Detect(root)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAdapter_DetectFromSkillsContainer(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Spec with no BaseDir — Detect must fall back to the skills container
	// derived from OutputPath. Mirrors agents that share `.agents/skills/`.
	spec := adept.HarnessSpec{
		ID:         "shared",
		Kind:       adept.KindPerSkill,
		OutputPath: ".agents/skills/{id}/SKILL.md",
	}
	a := perskill.NewAdapter(spec, fakeWriter{}, osLinker{})
	ok, _ := a.Detect(root)
	require.False(t, ok)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o755))
	ok, err := a.Detect(root)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAdapter_ValidateDriftAndSynced(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := perskill.NewAdapter(junieSpec(), fakeWriter{}, osLinker{})
	target := ".junie/skills/foo/SKILL.md"
	abs := filepath.Join(root, target)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("DIFFERENT"), 0o644))
	r, err := a.Validate(root, []adept.RenderOutput{
		{Path: target, Bytes: []byte("EXPECTED")},
	})
	require.NoError(t, err)
	require.Equal(t, []string{target}, r.Drifted)

	require.NoError(t, os.WriteFile(abs, []byte("EXPECTED"), 0o644))
	r, err = a.Validate(root, []adept.RenderOutput{
		{Path: target, Bytes: []byte("EXPECTED")},
	})
	require.NoError(t, err)
	require.Equal(t, []string{target}, r.Synced)
}

func TestAdapter_ImportRoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := perskill.NewAdapter(junieSpec(), fakeWriter{}, osLinker{})
	dir := filepath.Join(root, ".junie", "skills", "demo")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := "---\nname: demo\ndescription: imported\n---\nbody text\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644))

	got, err := a.Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "demo", got[0].Skill.ID)
	require.Equal(t, "imported", got[0].Skill.Description)
}

func TestAdapter_Aggregate_PassesThrough(t *testing.T) {
	t.Parallel()
	a := perskill.NewAdapter(junieSpec(), fakeWriter{}, osLinker{})
	in := []adept.RenderOutput{{Path: "a"}, {Path: "b"}}
	out, err := a.Aggregate(context.Background(), in, 0)
	require.NoError(t, err)
	require.Equal(t, in, out)
}
