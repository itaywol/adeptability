package cursor

// This file lives in package cursor (internal test) so it can exercise the
// unexported splitFrontmatter / fieldsFor helpers directly while still driving
// the exported Render / Import / Detect surface. It is intentionally named
// zcoverage_test.go so it never collides with the existing cursor_test files.

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

// --- fakes (uniquely named to avoid colliding with adapter_test.go) ---------

type zcovWriter struct{}

func (zcovWriter) AtomicWrite(_ string, _ []byte, _ fs.FileMode) error { return nil }
func (zcovWriter) EnsureDir(_ string) error                            { return nil }

// zcovScriptedLinker returns a scripted PathType per (relative) path so we can
// force the .cursor/rules fallback branch in Detect that an os-backed linker
// can never reach (a real ".cursor/rules" implies ".cursor" already exists).
type zcovScriptedLinker struct {
	// byBase maps the final path element (filepath.Base) to a PathType.
	byBase map[string]fsutil.PathType
}

func (l zcovScriptedLinker) SymlinkOrCopy(_ string, _ string, _ bool) (adept.HarnessMode, error) {
	return adept.ModeSymlink, nil
}
func (l zcovScriptedLinker) ReadSymlink(_ string) (string, error) { return "", nil }
func (l zcovScriptedLinker) PathType(p string) fsutil.PathType {
	// Detect joins projectRoot/.cursor and projectRoot/.cursor/rules. Key on the
	// trailing two elements so we can distinguish ".cursor" from ".cursor/rules".
	dir := filepath.Base(filepath.Dir(p))
	base := filepath.Base(p)
	if dir == ".cursor" && base == "rules" {
		if t, ok := l.byBase["rules"]; ok {
			return t
		}
		return common.PathMissing
	}
	if t, ok := l.byBase[base]; ok {
		return t
	}
	return common.PathMissing
}

var _ common.Linker = zcovScriptedLinker{}

func zcovAdapter(l common.Linker) *Adapter {
	return NewAdapter(New(), zcovWriter{}, l)
}

// --- splitFrontmatter (unexported) ------------------------------------------

func TestSplitFrontmatter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		raw       string
		wantFront string // "" means nil front
		wantNil   bool   // expect nil front slice
		wantBody  string
		wantErr   bool
	}{
		{
			name:      "well-formed split trims one leading body newline",
			raw:       "---\ndescription: hi\n---\nbody line\n",
			wantFront: "description: hi",
			wantBody:  "body line\n",
		},
		{
			name:     "no leading fence is passthrough body, nil front",
			raw:      "no frontmatter here\nsecond line\n",
			wantNil:  true,
			wantBody: "no frontmatter here\nsecond line\n",
		},
		{
			name:    "unterminated frontmatter errors",
			raw:     "---\ndescription: hi\nno closing fence\n",
			wantErr: true,
		},
		{
			name:      "closing fence at EOF yields empty body",
			raw:       "---\ndescription: hi\n---",
			wantFront: "description: hi",
			wantBody:  "",
		},
		{
			// REGRESSION: strings.Index(rest, "\n---") matches the FIRST "\n---",
			// so a body that itself contains a "---" thematic break terminates the
			// frontmatter early. Pin the documented (brittle) behavior so a future
			// matcher change is caught.
			name:      "body containing a thematic break terminates frontmatter at first match",
			raw:       "---\ndescription: hi\n---\nintro\n---\noutro\n",
			wantFront: "description: hi",
			// rest after first "\n---" is "\nintro\n---\noutro\n"; TrimPrefix("\n")
			// drops the single leading newline.
			wantBody: "intro\n---\noutro\n",
		},
		{
			// "----" (four dashes) still matches "\n---".
			name:      "four-dash line still matches the three-dash close",
			raw:       "---\ndescription: hi\n----\nbody\n",
			wantFront: "description: hi",
			wantBody:  "-\nbody\n",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			front, body, err := splitFrontmatter([]byte(tc.raw))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.wantNil {
				require.Nil(t, front)
			} else {
				require.Equal(t, tc.wantFront, string(front))
			}
			require.Equal(t, tc.wantBody, body)
		})
	}
}

// --- fieldsFor via Render: error + quoting ----------------------------------

func TestFieldsFor_ErrorAndQuoting(t *testing.T) {
	t.Parallel()
	r := New()

	t.Run("whitespace-only description is invalid", func(t *testing.T) {
		t.Parallel()
		_, err := r.Render(context.Background(), adept.RenderInput{
			Skill: &adept.Skill{ID: "x", Description: "   \t  ", Activation: adept.ActivationAgent},
		})
		require.ErrorIs(t, err, adept.ErrSkillInvalid)
		require.Contains(t, err.Error(), "description empty")
	})

	t.Run("unknown activation hits default case", func(t *testing.T) {
		t.Parallel()
		_, err := r.Render(context.Background(), adept.RenderInput{
			Skill: &adept.Skill{ID: "x", Description: "ok", Activation: adept.ActivationMode("bogus")},
		})
		require.ErrorIs(t, err, adept.ErrSkillInvalid)
		require.Contains(t, err.Error(), "unknown activation")
	})

	t.Run("description with colon or hash is quoted in frontmatter", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(context.Background(), adept.RenderInput{
			Skill: &adept.Skill{ID: "x", Description: "use: foo # bar", Activation: adept.ActivationAgent},
		})
		require.NoError(t, err)
		require.Contains(t, string(out.Bytes), `description: "use: foo # bar"`)
	})

	t.Run("plain description is not quoted", func(t *testing.T) {
		t.Parallel()
		out, err := r.Render(context.Background(), adept.RenderInput{
			Skill: &adept.Skill{ID: "x", Description: "plain words", Activation: adept.ActivationAgent},
		})
		require.NoError(t, err)
		require.Contains(t, string(out.Bytes), "description: plain words")
		require.NotContains(t, string(out.Bytes), `"plain words"`)
	})
}

// --- Render: ctx-cancelled + empty id ---------------------------------------

func TestRender_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := New().Render(ctx, adept.RenderInput{
		Skill: &adept.Skill{ID: "valid", Description: "valid", Activation: adept.ActivationAgent},
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, out.Bytes)
}

func TestRender_EmptyID(t *testing.T) {
	t.Parallel()
	_, err := New().Render(context.Background(), adept.RenderInput{
		Skill: &adept.Skill{ID: "", Description: "valid", Activation: adept.ActivationAgent},
	})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
	require.Contains(t, err.Error(), "skill id empty")
}

// --- Render: per-activation .mdc frontmatter shape + single-file sidecar -----

func TestRender_ActivationFrontmatterShapes(t *testing.T) {
	t.Parallel()
	r := New()
	cases := []struct {
		name        string
		skill       *adept.Skill
		mustContain []string
		mustNotHave []string
	}{
		{
			name:        "always",
			skill:       &adept.Skill{ID: "a", Description: "d", Activation: adept.ActivationAlways, Body: "B"},
			mustContain: []string{"alwaysApply: true", "description: d"},
			mustNotHave: []string{"globs:"},
		},
		{
			name:        "globs",
			skill:       &adept.Skill{ID: "g", Description: "d", Activation: adept.ActivationGlobs, Globs: []string{"*.go"}, Body: "B"},
			mustContain: []string{"globs:", "*.go", "alwaysApply: false", "description: d"},
		},
		{
			name:        "agent",
			skill:       &adept.Skill{ID: "ag", Description: "d", Activation: adept.ActivationAgent, Body: "B"},
			mustContain: []string{"description: d"},
			mustNotHave: []string{"alwaysApply", "globs:"},
		},
		{
			name:        "manual emits invoke hint",
			skill:       &adept.Skill{ID: "rotate", Description: "d", Activation: adept.ActivationManual, Body: "B"},
			mustContain: []string{"description: d", "@rotate"},
			mustNotHave: []string{"alwaysApply", "globs:"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := r.Render(context.Background(), adept.RenderInput{Skill: tc.skill})
			require.NoError(t, err)
			require.Equal(t, ".cursor/rules/"+tc.skill.ID+".mdc", out.Path)
			require.Equal(t, tc.skill.ID, out.SkillID)
			require.NotEmpty(t, out.SkillHash)
			s := string(out.Bytes)
			require.True(t, len(s) > 4 && s[:4] == "---\n", "expected leading frontmatter fence, got %q", s)
			for _, want := range tc.mustContain {
				require.Contains(t, s, want)
			}
			for _, bad := range tc.mustNotHave {
				require.NotContains(t, s, bad)
			}
		})
	}
}

func TestRender_SingleFileSidecarsDropped(t *testing.T) {
	t.Parallel()
	out, err := New().Render(context.Background(), adept.RenderInput{
		Skill: &adept.Skill{
			ID:          "s",
			Description: "d",
			Activation:  adept.ActivationAgent,
			Files: []adept.SkillFile{
				{RelPath: "scripts/h.sh", Bytes: []byte("x"), Mode: 0o755},
				{RelPath: "ref/a.md", Bytes: []byte("y"), Mode: 0o644},
			},
		},
	})
	require.NoError(t, err)
	require.Empty(t, out.Sidecars, "cursor is single-file: no sidecars emitted")
	require.Len(t, out.Warnings, 2)
	require.Contains(t, out.Warnings[0], "scripts/h.sh")
	require.Contains(t, out.Warnings[1], "ref/a.md")
}

// --- Adapter.Renderer accessor ----------------------------------------------

func TestAdapter_Renderer_ReturnsUnderlying(t *testing.T) {
	t.Parallel()
	r := New()
	a := NewAdapter(r, zcovWriter{}, zcovScriptedLinker{})
	got := a.Renderer()
	require.NotNil(t, got)
	require.Same(t, r, got, "Renderer() must return the exact *Renderer passed to NewAdapter")
}

// --- Adapter.Detect: nil linker + scripted fallback edges -------------------

func TestAdapter_Detect_NilLinkerAndEdges(t *testing.T) {
	t.Parallel()

	t.Run("nil linker errors with ErrAdapterInvalid", func(t *testing.T) {
		t.Parallel()
		a := NewAdapter(New(), zcovWriter{}, nil)
		ok, err := a.Detect("/whatever")
		require.False(t, ok)
		require.ErrorIs(t, err, adept.ErrAdapterInvalid)
	})

	t.Run(".cursor as a plain file still detects true", func(t *testing.T) {
		t.Parallel()
		a := zcovAdapter(zcovScriptedLinker{byBase: map[string]fsutil.PathType{".cursor": common.PathFile}})
		ok, err := a.Detect("/proj")
		require.NoError(t, err)
		require.True(t, ok, "PathType != PathMissing should detect even for a file")
	})

	t.Run(".cursor dir present detects true", func(t *testing.T) {
		t.Parallel()
		a := zcovAdapter(zcovScriptedLinker{byBase: map[string]fsutil.PathType{".cursor": common.PathDirectory}})
		ok, err := a.Detect("/proj")
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("fallback: .cursor missing but .cursor/rules present detects true", func(t *testing.T) {
		t.Parallel()
		// .cursor reports missing, .cursor/rules reports a directory: forces the
		// lines 48-51 fallback branch an os-backed linker can never reach.
		a := zcovAdapter(zcovScriptedLinker{byBase: map[string]fsutil.PathType{
			".cursor": common.PathMissing,
			"rules":   common.PathDirectory,
		}})
		ok, err := a.Detect("/proj")
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("both missing detects false", func(t *testing.T) {
		t.Parallel()
		a := zcovAdapter(zcovScriptedLinker{byBase: map[string]fsutil.PathType{}})
		ok, err := a.Detect("/proj")
		require.NoError(t, err)
		require.False(t, ok)
	})
}

// --- Import: reverse mapping, filtering, sorting, error/edge branches --------

func writeMDC(t *testing.T, root, id, content string) {
	t.Helper()
	dir := filepath.Join(root, ".cursor", "rules")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, id+".mdc"), []byte(content), 0o644))
}

func TestImport_ActivationReverseMapping(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeMDC(t, root, "alwaysone", "---\ndescription: A\nalwaysApply: true\n---\nbody-a\n")
	writeMDC(t, root, "globsone", "---\ndescription: G\nglobs:\n  - \"*.go\"\n  - \"*.md\"\n---\nbody-g\n")
	writeMDC(t, root, "agentone", "---\ndescription: AG\n---\nbody-ag\n")

	a := zcovAdapter(zcovScriptedLinker{})
	got, err := a.Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, got, 3)

	byID := map[string]*adept.Skill{}
	for _, s := range got {
		byID[s.Skill.ID] = s.Skill
		require.Equal(t, filepath.Join(root, ".cursor", "rules", s.Skill.ID+".mdc"), s.SourcePath)
	}

	require.Equal(t, adept.ActivationAlways, byID["alwaysone"].Activation)
	require.Equal(t, "A", byID["alwaysone"].Description)
	// splitFrontmatter strips one leading newline but preserves the rest of the
	// body verbatim, including the trailing newline.
	require.Equal(t, "body-a\n", byID["alwaysone"].Body)

	require.Equal(t, adept.ActivationGlobs, byID["globsone"].Activation)
	require.Equal(t, []string{"*.go", "*.md"}, byID["globsone"].Globs)

	require.Equal(t, adept.ActivationAgent, byID["agentone"].Activation)
	require.Empty(t, byID["agentone"].Globs)
}

func TestImport_EmptyDescriptionSynthesized(t *testing.T) {
	t.Parallel()

	t.Run("frontmatter without description", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeMDC(t, root, "nodesc", "---\nalwaysApply: true\n---\nbody\n")
		got, err := zcovAdapter(zcovScriptedLinker{}).Import(context.Background(), root)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "Imported from Cursor nodesc", got[0].Skill.Description)
	})

	t.Run("no frontmatter at all", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeMDC(t, root, "plain", "just a body, no fence\n")
		got, err := zcovAdapter(zcovScriptedLinker{}).Import(context.Background(), root)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "Imported from Cursor plain", got[0].Skill.Description)
		require.Equal(t, adept.ActivationAgent, got[0].Skill.Activation)
		require.Equal(t, "just a body, no fence\n", got[0].Skill.Body)
	})
}

func TestImport_FiltersAndSorts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, ".cursor", "rules")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	// out-of-order valid .mdc files
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.mdc"), []byte("---\ndescription: B\n---\nb\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.mdc"), []byte("---\ndescription: A\n---\na\n"), 0o644))
	// non-.mdc file (filtered)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("nope"), 0o644))
	// a directory whose name ends in .mdc (filtered by e.IsDir())
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "weird.mdc"), 0o755))

	got, err := zcovAdapter(zcovScriptedLinker{}).Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "a", got[0].Skill.ID)
	require.Equal(t, "b", got[1].Skill.ID)
	require.True(t, filepath.IsAbs(got[0].SourcePath))
}

func TestImport_MissingDirAndBadFrontmatter(t *testing.T) {
	t.Parallel()

	t.Run("missing .cursor/rules dir returns nil,nil", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir() // no .cursor/rules created
		got, err := zcovAdapter(zcovScriptedLinker{}).Import(context.Background(), root)
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("unterminated frontmatter wraps skill id", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeMDC(t, root, "broken", "---\ndescription: x\nno closing fence here\n")
		_, err := zcovAdapter(zcovScriptedLinker{}).Import(context.Background(), root)
		require.Error(t, err)
		require.Contains(t, err.Error(), "broken")
		require.Contains(t, err.Error(), "unterminated frontmatter")
	})
}

func TestImport_BadYAMLFrontmatter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Invalid YAML inside an otherwise well-terminated frontmatter block.
	writeMDC(t, root, "badyaml", "---\nglobs: [unclosed\n---\nbody\n")
	_, err := zcovAdapter(zcovScriptedLinker{}).Import(context.Background(), root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "badyaml")
	require.Contains(t, err.Error(), "parse frontmatter")
}

// --- Render <-> Import round-trip (regression guard) ------------------------

func TestRenderImportRoundTrip(t *testing.T) {
	t.Parallel()
	r := New()
	cases := []struct {
		id    string
		skill *adept.Skill
	}{
		{"always-rt", &adept.Skill{ID: "always-rt", Description: "Always desc", Activation: adept.ActivationAlways, Body: "always body\nline two"}},
		{"globs-rt", &adept.Skill{ID: "globs-rt", Description: "Globs desc", Activation: adept.ActivationGlobs, Globs: []string{"*.go", "internal/**"}, Body: "globs body"}},
		{"agent-rt", &adept.Skill{ID: "agent-rt", Description: "Agent desc", Activation: adept.ActivationAgent, Body: "agent body"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			out, err := r.Render(context.Background(), adept.RenderInput{Skill: tc.skill})
			require.NoError(t, err)

			root := t.TempDir()
			writeMDC(t, root, tc.id, string(out.Bytes))

			imported, err := zcovAdapter(zcovScriptedLinker{}).Import(context.Background(), root)
			require.NoError(t, err)
			require.Len(t, imported, 1)
			rec := imported[0].Skill

			require.Equal(t, tc.skill.ID, rec.ID)
			require.Equal(t, tc.skill.Activation, rec.Activation, "activation must survive round-trip")
			require.Equal(t, tc.skill.Description, rec.Description, "description must survive round-trip")
			require.Equal(t, tc.skill.Globs, rec.Globs)
			// Body round-trip asymmetry (documented, pinned): Render trims the
			// body's trailing newlines, then emits a blank line after the closing
			// "---" fence followed by "<body>\n" ("...---\n\n<body>\n").
			// splitFrontmatter only strips a single leading "\n" via TrimPrefix, so
			// the recovered body gains one leading newline AND a trailing newline.
			// If the renderer's spacing or the importer's trim changes, this fires.
			wantBody := "\n" + strings.TrimRight(tc.skill.Body, "\n") + "\n"
			require.Equal(t, wantBody, rec.Body,
				"body round-trips with leading+trailing newline due to render/import spacing asymmetry")
		})
	}
}
