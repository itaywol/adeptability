package canonical

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func newTestLoader(t *testing.T) Loader {
	t.Helper()
	v, err := NewValidator()
	require.NoError(t, err)
	return NewLoader(NewParser(), v)
}

func relPaths(files []adept.SkillFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.RelPath
	}
	return out
}

func TestLoader_ValidYAML(t *testing.T) {
	l := newTestLoader(t)
	s, err := l.LoadSkillDir("testdata/valid-yaml")
	require.NoError(t, err)
	require.Equal(t, "valid_yaml_skill", s.ID)
	require.Equal(t, 1, s.Version)
	require.NotEmpty(t, s.Body)
	require.Empty(t, s.Files)
}

func TestLoader_ValidFrontmatter(t *testing.T) {
	l := newTestLoader(t)
	s, err := l.LoadSkillDir("testdata/valid-frontmatter")
	require.NoError(t, err)
	require.Equal(t, "valid_frontmatter_skill", s.ID)
	require.Equal(t, 2, s.Version)
	require.Equal(t, adept.ActivationAlways, s.Activation)
	require.Contains(t, s.Body, "# Frontmatter Skill")
}

func TestLoader_MissingBoth(t *testing.T) {
	l := newTestLoader(t)
	_, err := l.LoadSkillDir("testdata/missing-both")
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestLoader_InvalidID(t *testing.T) {
	l := newTestLoader(t)
	_, err := l.LoadSkillDir("testdata/invalid-id")
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestLoader_MissingDescription(t *testing.T) {
	l := newTestLoader(t)
	_, err := l.LoadSkillDir("testdata/missing-description")
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestLoader_GlobsWithoutGlobs(t *testing.T) {
	l := newTestLoader(t)
	_, err := l.LoadSkillDir("testdata/globs-without-globs")
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestLoader_SidecarsDiscovered(t *testing.T) {
	l := newTestLoader(t)
	s, err := l.LoadSkillDir("testdata/with-sidecars")
	require.NoError(t, err)
	paths := relPaths(s.Files)
	require.ElementsMatch(t, []string{"references/notes.md", "scripts/run.sh"}, paths)
}

func TestLoader_IgnoreExcludesFiles(t *testing.T) {
	l := newTestLoader(t)
	s, err := l.LoadSkillDir("testdata/with-ignore")
	require.NoError(t, err)
	paths := relPaths(s.Files)
	require.Contains(t, paths, "scripts/run.sh")
	require.NotContains(t, paths, "scripts/secret.sh")
	require.NotContains(t, paths, "build.log")
}

func TestLoader_NotADirectory(t *testing.T) {
	l := newTestLoader(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "skill")
	require.NoError(t, os.WriteFile(f, []byte("not a dir"), 0o644))
	_, err := l.LoadSkillDir(f)
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestLoader_NotFound(t *testing.T) {
	l := newTestLoader(t)
	_, err := l.LoadSkillDir(filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillNotFound)
}

func TestLoader_StripsRedundantFrontmatterWhenYAMLPresent(t *testing.T) {
	l := newTestLoader(t)
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, adept.SkillYAMLName), []byte(
		"id: yaml_first\nversion: 1\ndescription: yaml wins\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, adept.SkillFileName), []byte(
		"---\nid: other\nversion: 99\ndescription: ignored\n---\n# Real body\n"), 0o644))
	s, err := l.LoadSkillDir(tmp)
	require.NoError(t, err)
	require.Equal(t, "yaml_first", s.ID)
	require.Contains(t, s.Body, "# Real body")
	require.NotContains(t, s.Body, "ignored")
}
