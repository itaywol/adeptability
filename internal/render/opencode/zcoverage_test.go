package opencode_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itaywol/adeptability/internal/render/opencode"
	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

// writeFile is a small helper that creates parent dirs and writes a file with
// the given mode, failing the test on any error.
func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, data, mode))
	// WriteFile honours the umask, so chmod explicitly to lock the perm bits.
	require.NoError(t, os.Chmod(path, mode))
}

// TestSplitOpenCodeMarkdown exercises splitOpenCodeMarkdown indirectly through
// Adapter.Import. The function is unexported; importing real SKILL.md files and
// asserting the parsed Description/Body covers every branch of the splitter.
func TestSplitOpenCodeMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       string
		content  string
		wantDesc string
		wantBody string
	}{
		{
			name:     "standard heading description body",
			id:       "alpha",
			content:  "# alpha\n\ndescription here\n\nbody line one\nbody line two\n",
			wantDesc: "description here",
			wantBody: "body line one\nbody line two\n",
		},
		{
			name:     "no heading first non-empty line is description",
			id:       "beta",
			content:  "just a description\n\nthe body\n",
			wantDesc: "just a description",
			wantBody: "the body\n",
		},
		{
			name:     "empty input falls back to imported-from label",
			id:       "gamma",
			content:  "",
			wantDesc: "Imported from OpenCode gamma",
			wantBody: "",
		},
		{
			name:     "whitespace-only input falls back to imported-from label",
			id:       "delta",
			content:  "   \n\n\t\n",
			wantDesc: "Imported from OpenCode delta",
			wantBody: "",
		},
		{
			name:     "heading and description but no body",
			id:       "epsilon",
			content:  "# epsilon\n\nonly a description\n",
			wantDesc: "only a description",
			wantBody: "",
		},
		{
			name:     "description immediately followed by body with no blank line",
			id:       "zeta",
			content:  "# zeta\nthe description\nbody right after\nmore body\n",
			wantDesc: "the description",
			wantBody: "body right after\nmore body\n",
		},
		{
			name:     "multiple leading blank lines after heading are skipped",
			id:       "eta",
			content:  "# eta\n\n\n\nlate description\n\nlate body\n",
			wantDesc: "late description",
			wantBody: "late body\n",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".opencode", "skill", tt.id, adept.SkillFileName), []byte(tt.content), 0o644)

			got, err := newAdapter().Import(context.Background(), root)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, tt.id, got[0].Skill.ID)
			require.Equal(t, tt.wantDesc, got[0].Skill.Description)
			require.Equal(t, tt.wantBody, got[0].Skill.Body)
		})
	}
}

// TestCollectSidecars verifies sidecar discovery: SKILL.md is excluded, dotfiles
// and dot-directories are skipped (SkipDir), nested rel-paths are slash-joined,
// file permissions are preserved, and the result is sorted by RelPath.
func TestCollectSidecars(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillDir := filepath.Join(root, ".opencode", "skill", "tools")

	writeFile(t, filepath.Join(skillDir, adept.SkillFileName), []byte("# tools\n\ndesc\n"), 0o644)
	writeFile(t, filepath.Join(skillDir, "scripts", "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755)
	writeFile(t, filepath.Join(skillDir, "docs", "ref.md"), []byte("reference\n"), 0o644)
	writeFile(t, filepath.Join(skillDir, ".hidden"), []byte("secret\n"), 0o644)
	// A dot-directory and its content must be skipped via filepath.SkipDir.
	writeFile(t, filepath.Join(skillDir, ".git", "config"), []byte("[core]\n"), 0o644)

	got, err := newAdapter().Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, got, 1)
	files := got[0].Files

	rels := make([]string, len(files))
	for i, f := range files {
		rels[i] = f.RelPath
	}
	// SKILL.md, .hidden, and everything under .git/ are excluded; remainder sorted.
	require.Equal(t, []string{"docs/ref.md", "scripts/run.sh"}, rels)

	byRel := map[string]adept.SkillFile{}
	for _, f := range files {
		byRel[f.RelPath] = f
	}
	require.Equal(t, os.FileMode(0o755), byRel["scripts/run.sh"].Mode.Perm())
	require.Equal(t, os.FileMode(0o644), byRel["docs/ref.md"].Mode.Perm())
	require.Equal(t, "#!/bin/sh\necho hi\n", string(byRel["scripts/run.sh"].Bytes))

	// Forward-slash separators even though the OS may use backslashes natively.
	for _, f := range files {
		require.NotContains(t, f.RelPath, "\\")
	}
}

// TestCollectSidecars_EmptySkillDir confirms a skill directory holding only
// SKILL.md yields a nil/empty Files slice rather than an error.
func TestCollectSidecars_EmptySkillDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".opencode", "skill", "lonely", adept.SkillFileName), []byte("# lonely\n\nd\n"), 0o644)

	got, err := newAdapter().Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Empty(t, got[0].Files)
}

// TestAdapter_Import_HappyPath covers the core import contract: deterministic
// sort by skill ID, ActivationAgent always set, SourcePath pointing at SKILL.md,
// and sidecars carried for the per-skill model.
func TestAdapter_Import_HappyPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillBase := filepath.Join(root, ".opencode", "skill")

	// Created beta before alpha to prove the output is sorted, not insertion-ordered.
	writeFile(t, filepath.Join(skillBase, "beta", adept.SkillFileName), []byte("# beta\n\nbeta desc\n\nbeta body\n"), 0o644)
	writeFile(t, filepath.Join(skillBase, "alpha", adept.SkillFileName), []byte("# alpha\n\nalpha desc\n\nalpha body\n"), 0o644)
	writeFile(t, filepath.Join(skillBase, "alpha", "assets", "data.txt"), []byte("payload\n"), 0o644)

	got, err := newAdapter().Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, got, 2)

	require.Equal(t, "alpha", got[0].Skill.ID)
	require.Equal(t, "beta", got[1].Skill.ID)

	for _, imp := range got {
		require.Equal(t, adept.ActivationAgent, imp.Skill.Activation)
	}

	require.Equal(t, "alpha desc", got[0].Skill.Description)
	require.Equal(t, "alpha body\n", got[0].Skill.Body)
	require.Equal(t, filepath.Join(skillBase, "alpha", adept.SkillFileName), got[0].SourcePath)
	require.Len(t, got[0].Files, 1)
	require.Equal(t, "assets/data.txt", got[0].Files[0].RelPath)

	require.Equal(t, "beta desc", got[1].Skill.Description)
	require.Empty(t, got[1].Files)
}

// TestAdapter_Import_MissingBaseDir asserts the os.IsNotExist branch: a project
// with no .opencode/skill directory imports cleanly as (nil, nil).
func TestAdapter_Import_MissingBaseDir(t *testing.T) {
	t.Parallel()
	got, err := newAdapter().Import(context.Background(), t.TempDir())
	require.NoError(t, err)
	require.Nil(t, got)
}

// TestAdapter_Import_SkipsNonDirAndMissingSkillFile proves Import tolerates a
// partially-populated tree: a stray regular file in the skill base is skipped,
// a directory lacking SKILL.md is skipped, and only valid skills are returned.
func TestAdapter_Import_SkipsNonDirAndMissingSkillFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillBase := filepath.Join(root, ".opencode", "skill")
	require.NoError(t, os.MkdirAll(skillBase, 0o755))

	// Stray non-directory entry directly under the skill base.
	require.NoError(t, os.WriteFile(filepath.Join(skillBase, "README"), []byte("not a skill\n"), 0o644))
	// Directory with no SKILL.md.
	require.NoError(t, os.MkdirAll(filepath.Join(skillBase, "empty"), 0o755))
	// One valid skill.
	writeFile(t, filepath.Join(skillBase, "valid", adept.SkillFileName), []byte("# valid\n\nv\n"), 0o644)

	got, err := newAdapter().Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "valid", got[0].Skill.ID)
}

// TestAdapter_Detect_NilLinker covers the guard branch: a nil Linker makes
// Detect return ErrAdapterInvalid rather than panicking.
func TestAdapter_Detect_NilLinker(t *testing.T) {
	t.Parallel()
	a := opencode.NewAdapter(opencode.New(), fakeWriter{}, nil)
	ok, err := a.Detect(t.TempDir())
	require.False(t, ok)
	require.ErrorIs(t, err, adept.ErrAdapterInvalid)
}

// TestAdapter_Detect_Branches exercises both PathType branches of Detect:
// (1) .opencode present as a plain file (no skill dir) still detects true;
// (2) only .opencode/skill present (no other .opencode marker) detects true.
func TestAdapter_Detect_Branches(t *testing.T) {
	t.Parallel()

	t.Run("opencode file but no skill dir", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		// .opencode exists as a plain file, not a directory.
		require.NoError(t, os.WriteFile(filepath.Join(root, ".opencode"), []byte("x"), 0o644))
		ok, err := newAdapter().Detect(root)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("skill dir only", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".opencode", "skill"), 0o755))
		ok, err := newAdapter().Detect(root)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("nothing present", func(t *testing.T) {
		t.Parallel()
		ok, err := newAdapter().Detect(t.TempDir())
		require.NoError(t, err)
		require.False(t, ok)
	})
}

// TestAdapter_Renderer_ReturnsUnderlying closes the accessor: Renderer() must
// return the exact *opencode.Renderer passed to NewAdapter, unwrapped.
func TestAdapter_Renderer_ReturnsUnderlying(t *testing.T) {
	t.Parallel()
	r := opencode.New()
	a := opencode.NewAdapter(r, fakeWriter{}, osLinker{})
	got := a.Renderer()
	require.NotNil(t, got)
	gotR, ok := got.(*opencode.Renderer)
	require.True(t, ok, "Renderer() must return *opencode.Renderer")
	require.Same(t, r, gotR)
}

// TestAdapter_Import_RoundTripWithRender locks the render<->import inverse:
// rendering a skill to disk and importing it back must recover the same ID,
// Description, Body (modulo trailing-newline), and sidecars. This guards against
// divergence between the renderer's heading/description layout and the splitter.
func TestAdapter_Import_RoundTripWithRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		skill *adept.Skill
	}{
		{
			name: "with body and sidecar",
			skill: &adept.Skill{
				ID:          "round-trip",
				Description: "a round-trip description",
				Activation:  adept.ActivationAlways,
				Body:        "First paragraph.\n\nSecond paragraph.",
				Files: []adept.SkillFile{
					{RelPath: "scripts/run.sh", Bytes: []byte("#!/bin/sh\n"), Mode: 0o755},
				},
			},
		},
		{
			name: "description only no body",
			skill: &adept.Skill{
				ID:          "no-body",
				Description: "just a description",
				Activation:  adept.ActivationManual,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := opencode.New()
			out, err := r.Render(context.Background(), adept.RenderInput{
				Skill:   tt.skill,
				Harness: opencode.Spec(),
			})
			require.NoError(t, err)

			root := t.TempDir()
			dst := filepath.Join(root, ".opencode", "skill", tt.skill.ID, adept.SkillFileName)
			writeFile(t, dst, out.Bytes, os.FileMode(0o644))
			for _, sc := range out.Sidecars {
				writeFile(t, filepath.Join(root, ".opencode", "skill", tt.skill.ID, sc.RelPath), sc.Bytes, sc.Mode.Perm())
			}

			got, err := newAdapter().Import(context.Background(), root)
			require.NoError(t, err)
			require.Len(t, got, 1)
			imp := got[0]

			require.Equal(t, tt.skill.ID, imp.Skill.ID)
			require.Equal(t, tt.skill.Description, imp.Skill.Description)
			// Activation is intentionally not preserved by OpenCode (directory-based);
			// import always assigns ActivationAgent.
			require.Equal(t, adept.ActivationAgent, imp.Skill.Activation)
			// Body recovered modulo trailing-newline normalization.
			require.Equal(t, strings.TrimRight(tt.skill.Body, "\n"), strings.TrimRight(imp.Skill.Body, "\n"))

			require.Len(t, imp.Files, len(tt.skill.Files))
			for i, f := range tt.skill.Files {
				require.Equal(t, f.RelPath, imp.Files[i].RelPath)
				require.Equal(t, f.Bytes, imp.Files[i].Bytes)
				require.Equal(t, f.Mode.Perm(), imp.Files[i].Mode.Perm())
			}
		})
	}
}
