package merge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeTree materializes a side directory with the given path → bytes
// map for use in table-driven tests. Forward-slash relative paths are
// translated to OS paths.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(root, 0o755))
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
}

func newSides(t *testing.T) (base, ours, theirs string) {
	t.Helper()
	root := t.TempDir()
	base = filepath.Join(root, "base")
	ours = filepath.Join(root, "ours")
	theirs = filepath.Join(root, "theirs")
	require.NoError(t, os.MkdirAll(base, 0o755))
	require.NoError(t, os.MkdirAll(ours, 0o755))
	require.NoError(t, os.MkdirAll(theirs, 0o755))
	return
}

type mergeCase struct {
	name        string
	base        map[string]string
	ours        map[string]string
	theirs      map[string]string
	wantFiles   map[string]string // expected merged bytes
	wantDeletes []string          // expected deleted paths
	wantConfs   []string          // relative paths that must be flagged conflict
}

func TestMerger_Table(t *testing.T) {
	cases := []mergeCase{
		{
			name:      "no changes",
			base:      map[string]string{"SKILL.md": "A\nB\nC\n"},
			ours:      map[string]string{"SKILL.md": "A\nB\nC\n"},
			theirs:    map[string]string{"SKILL.md": "A\nB\nC\n"},
			wantFiles: map[string]string{"SKILL.md": "A\nB\nC\n"},
		},
		{
			name:      "only ours changed",
			base:      map[string]string{"SKILL.md": "A\nB\nC\n"},
			ours:      map[string]string{"SKILL.md": "A\nX\nC\n"},
			theirs:    map[string]string{"SKILL.md": "A\nB\nC\n"},
			wantFiles: map[string]string{"SKILL.md": "A\nX\nC\n"},
		},
		{
			name:      "only theirs changed",
			base:      map[string]string{"SKILL.md": "A\nB\nC\n"},
			ours:      map[string]string{"SKILL.md": "A\nB\nC\n"},
			theirs:    map[string]string{"SKILL.md": "A\nB\nY\n"},
			wantFiles: map[string]string{"SKILL.md": "A\nB\nY\n"},
		},
		{
			name:      "both made the same change",
			base:      map[string]string{"SKILL.md": "A\nB\nC\n"},
			ours:      map[string]string{"SKILL.md": "A\nZ\nC\n"},
			theirs:    map[string]string{"SKILL.md": "A\nZ\nC\n"},
			wantFiles: map[string]string{"SKILL.md": "A\nZ\nC\n"},
		},
		{
			name:      "non-overlapping edits merge cleanly",
			base:      map[string]string{"SKILL.md": "A\nB\nC\nD\nE\n"},
			ours:      map[string]string{"SKILL.md": "A\nB-edit\nC\nD\nE\n"},
			theirs:    map[string]string{"SKILL.md": "A\nB\nC\nD-edit\nE\n"},
			wantFiles: map[string]string{"SKILL.md": "A\nB-edit\nC\nD-edit\nE\n"},
		},
		{
			name:      "additions on opposite ends merge cleanly",
			base:      map[string]string{"SKILL.md": "A\nB\nC\n"},
			ours:      map[string]string{"SKILL.md": "PRE\nA\nB\nC\n"},
			theirs:    map[string]string{"SKILL.md": "A\nB\nC\nPOST\n"},
			wantFiles: map[string]string{"SKILL.md": "PRE\nA\nB\nC\nPOST\n"},
		},
		{
			name:      "divergent edits on same line produce conflict markers",
			base:      map[string]string{"SKILL.md": "A\nB\nC\n"},
			ours:      map[string]string{"SKILL.md": "A\nOURS\nC\n"},
			theirs:    map[string]string{"SKILL.md": "A\nTHEIRS\nC\n"},
			wantConfs: []string{"SKILL.md"},
		},
		{
			name:      "file added only by ours",
			base:      map[string]string{"SKILL.md": "A\n"},
			ours:      map[string]string{"SKILL.md": "A\n", "new.md": "hello\n"},
			theirs:    map[string]string{"SKILL.md": "A\n"},
			wantFiles: map[string]string{"SKILL.md": "A\n", "new.md": "hello\n"},
		},
		{
			name:      "file added only by theirs",
			base:      map[string]string{"SKILL.md": "A\n"},
			ours:      map[string]string{"SKILL.md": "A\n"},
			theirs:    map[string]string{"SKILL.md": "A\n", "new.md": "hello\n"},
			wantFiles: map[string]string{"SKILL.md": "A\n", "new.md": "hello\n"},
		},
		{
			name:      "file added on both sides with identical content",
			base:      map[string]string{"SKILL.md": "A\n"},
			ours:      map[string]string{"SKILL.md": "A\n", "x.md": "same\n"},
			theirs:    map[string]string{"SKILL.md": "A\n", "x.md": "same\n"},
			wantFiles: map[string]string{"SKILL.md": "A\n", "x.md": "same\n"},
		},
		{
			name:      "file added on both sides with divergent content -> conflict",
			base:      map[string]string{"SKILL.md": "A\n"},
			ours:      map[string]string{"SKILL.md": "A\n", "x.md": "ours\n"},
			theirs:    map[string]string{"SKILL.md": "A\n", "x.md": "theirs\n"},
			wantConfs: []string{"x.md"},
		},
		{
			name:        "file deleted by ours, unchanged in theirs -> delete",
			base:        map[string]string{"SKILL.md": "A\n", "tmp.md": "stale\n"},
			ours:        map[string]string{"SKILL.md": "A\n"},
			theirs:      map[string]string{"SKILL.md": "A\n", "tmp.md": "stale\n"},
			wantFiles:   map[string]string{"SKILL.md": "A\n"},
			wantDeletes: []string{"tmp.md"},
		},
		{
			name:        "file deleted by theirs, unchanged in ours -> delete",
			base:        map[string]string{"SKILL.md": "A\n", "tmp.md": "stale\n"},
			ours:        map[string]string{"SKILL.md": "A\n", "tmp.md": "stale\n"},
			theirs:      map[string]string{"SKILL.md": "A\n"},
			wantFiles:   map[string]string{"SKILL.md": "A\n"},
			wantDeletes: []string{"tmp.md"},
		},
		{
			name:      "file deleted by ours but modified in theirs -> conflict",
			base:      map[string]string{"SKILL.md": "A\n", "tmp.md": "v1\n"},
			ours:      map[string]string{"SKILL.md": "A\n"},
			theirs:    map[string]string{"SKILL.md": "A\n", "tmp.md": "v2\n"},
			wantConfs: []string{"tmp.md"},
		},
		{
			name:      "file deleted by theirs but modified in ours -> conflict",
			base:      map[string]string{"SKILL.md": "A\n", "tmp.md": "v1\n"},
			ours:      map[string]string{"SKILL.md": "A\n", "tmp.md": "v2\n"},
			theirs:    map[string]string{"SKILL.md": "A\n"},
			wantConfs: []string{"tmp.md"},
		},
		{
			name:        "file deleted on both sides -> drop",
			base:        map[string]string{"SKILL.md": "A\n", "tmp.md": "v1\n"},
			ours:        map[string]string{"SKILL.md": "A\n"},
			theirs:      map[string]string{"SKILL.md": "A\n"},
			wantFiles:   map[string]string{"SKILL.md": "A\n"},
			wantDeletes: []string{"tmp.md"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base, ours, theirs := newSides(t)
			writeTree(t, base, tc.base)
			writeTree(t, ours, tc.ours)
			writeTree(t, theirs, tc.theirs)

			m := NewMerger()
			res, err := m.Merge(base, ours, theirs)
			require.NoError(t, err)

			conflictSet := map[string]bool{}
			for _, c := range res.Conflicts {
				require.True(t, c.HasConflict)
				conflictSet[c.Path] = true
			}
			for _, want := range tc.wantConfs {
				require.Truef(t, conflictSet[want], "expected conflict on %s, got conflicts: %v", want, res.Conflicts)
			}
			require.Equalf(t, len(tc.wantConfs), len(res.Conflicts), "extra conflicts: %+v", res.Conflicts)

			// Verify ResultFile entries match expectations.
			got := map[string]string{}
			deletes := map[string]bool{}
			for _, f := range res.Files {
				if f.Deleted {
					deletes[f.RelPath] = true
					continue
				}
				got[f.RelPath] = string(f.Bytes)
			}
			for _, d := range tc.wantDeletes {
				require.Truef(t, deletes[d], "expected delete for %s, got files: %+v", d, res.Files)
			}
			if len(tc.wantConfs) == 0 {
				for rel, want := range tc.wantFiles {
					require.Equalf(t, want, got[rel], "merge mismatch for %s", rel)
				}
			} else {
				// In conflict cases the merged file should contain the
				// canonical marker prefix.
				for _, p := range tc.wantConfs {
					body := got[p]
					require.Containsf(t, body, "<<<<<<< ours", "expected conflict markers in %s, got: %q", p, body)
					require.Containsf(t, body, ">>>>>>> theirs", "expected closing marker in %s", p)
				}
			}
		})
	}
}

func TestMerger_Deterministic(t *testing.T) {
	t.Parallel()
	base, ours, theirs := newSides(t)
	writeTree(t, base, map[string]string{
		"SKILL.md": "A\nB\nC\nD\nE\n",
		"x.md":     "hello\n",
	})
	writeTree(t, ours, map[string]string{
		"SKILL.md": "A\nB-ours\nC\nD\nE\n",
		"x.md":     "hello\n",
	})
	writeTree(t, theirs, map[string]string{
		"SKILL.md": "A\nB\nC\nD-theirs\nE\n",
		"x.md":     "hello\n",
	})
	m := NewMerger()
	first, err := m.Merge(base, ours, theirs)
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		again, err := m.Merge(base, ours, theirs)
		require.NoError(t, err)
		require.Equal(t, len(first.Files), len(again.Files))
		for j := range first.Files {
			require.Equal(t, first.Files[j].RelPath, again.Files[j].RelPath)
			require.Equal(t, first.Files[j].Bytes, again.Files[j].Bytes)
			require.Equal(t, first.Files[j].Conflict, again.Files[j].Conflict)
			require.Equal(t, first.Files[j].Deleted, again.Files[j].Deleted)
		}
	}
}

func TestMerger_BinaryFile_HashEquality(t *testing.T) {
	t.Parallel()
	base, ours, theirs := newSides(t)
	// Same binary content on every side: hash equality → no merge,
	// take any side without invoking diff3.
	binData := []byte{0x00, 0x01, 0x02, 0x00, 0xFF}
	require.NoError(t, os.WriteFile(filepath.Join(base, "blob.bin"), binData, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ours, "blob.bin"), binData, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(theirs, "blob.bin"), binData, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(base, "SKILL.md"), []byte("ok\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ours, "SKILL.md"), []byte("ok\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(theirs, "SKILL.md"), []byte("ok\n"), 0o644))

	m := NewMerger()
	res, err := m.Merge(base, ours, theirs)
	require.NoError(t, err)
	require.Empty(t, res.Conflicts)
	var got []byte
	for _, f := range res.Files {
		if f.RelPath == "blob.bin" {
			got = f.Bytes
		}
	}
	require.Equal(t, binData, got)
}

func TestMerger_BinaryFile_DivergentNoLineMerge(t *testing.T) {
	t.Parallel()
	base, ours, theirs := newSides(t)
	require.NoError(t, os.WriteFile(filepath.Join(base, "SKILL.md"), []byte("ok\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ours, "SKILL.md"), []byte("ok\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(theirs, "SKILL.md"), []byte("ok\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(base, "blob.bin"), []byte{0x00, 0x01}, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ours, "blob.bin"), []byte{0x00, 0x02}, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(theirs, "blob.bin"), []byte{0x00, 0x03}, 0o644))

	m := NewMerger()
	res, err := m.Merge(base, ours, theirs)
	require.NoError(t, err)
	require.Len(t, res.Conflicts, 1)
	require.Equal(t, "blob.bin", res.Conflicts[0].Path)
	// The conflict envelope must reference sha256 (not a line-merge).
	for _, f := range res.Files {
		if f.RelPath == "blob.bin" {
			require.True(t, f.Conflict)
			require.Contains(t, string(f.Bytes), "sha256=")
			require.NotContains(t, string(f.Bytes), "binary file blob.bin (2 bytes, sha256=)") // sanity
		}
	}
}

func TestMerger_EmptyBase(t *testing.T) {
	t.Parallel()
	base, ours, theirs := newSides(t)
	// base is empty; ours and theirs share one identical file.
	require.NoError(t, os.WriteFile(filepath.Join(ours, "SKILL.md"), []byte("same\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(theirs, "SKILL.md"), []byte("same\n"), 0o644))
	m := NewMerger()
	res, err := m.Merge(base, ours, theirs)
	require.NoError(t, err)
	require.Empty(t, res.Conflicts)
	require.Len(t, res.Files, 1)
	require.Equal(t, "SKILL.md", res.Files[0].RelPath)
	require.Equal(t, "same\n", string(res.Files[0].Bytes))
}

func TestMerger_NoBaseDir_TreatsAsEmpty(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	missingBase := filepath.Join(root, "no-such-base")
	ours := filepath.Join(root, "ours")
	theirs := filepath.Join(root, "theirs")
	require.NoError(t, os.MkdirAll(ours, 0o755))
	require.NoError(t, os.MkdirAll(theirs, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ours, "SKILL.md"), []byte("hi\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(theirs, "SKILL.md"), []byte("hi\n"), 0o644))
	m := NewMerger()
	res, err := m.Merge(missingBase, ours, theirs)
	require.NoError(t, err)
	require.Empty(t, res.Conflicts)
}

func TestDiff3ConflictMarkerOrder(t *testing.T) {
	// Hand-craft a conflict and inspect the exact marker order.
	base := []string{"A\n", "B\n", "C\n"}
	ours := []string{"A\n", "OURS\n", "C\n"}
	theirs := []string{"A\n", "THEIRS\n", "C\n"}
	out, confl := diff3Merge(ours, base, theirs, Options{
		MarkerOurs:   defaultMarkerOurs,
		MarkerBase:   defaultMarkerBase,
		MarkerTheirs: defaultMarkerTheirs,
		MarkerEnd:    defaultMarkerEnd,
	})
	require.True(t, confl)
	body := string(out)
	require.True(t, strings.HasPrefix(body, "A\n"), body)
	ourIdx := strings.Index(body, defaultMarkerOurs)
	baseIdx := strings.Index(body, defaultMarkerBase)
	theirsIdx := strings.Index(body, defaultMarkerTheirs)
	endIdx := strings.Index(body, defaultMarkerEnd)
	require.True(t, ourIdx < baseIdx && baseIdx < theirsIdx && theirsIdx < endIdx, body)
	require.Contains(t, body, "OURS\n")
	require.Contains(t, body, "B\n")
	require.Contains(t, body, "THEIRS\n")
	// The trailing stable "C\n" line must survive after the conflict block.
	require.True(t, strings.HasSuffix(body, "C\n"), body)
}

func TestSplitLines_PreservesTerminator(t *testing.T) {
	got := splitLines([]byte("A\nB\nC"))
	require.Equal(t, []string{"A\n", "B\n", "C"}, got)
	require.Nil(t, splitLines(nil))
	require.Equal(t, []string{"\n"}, splitLines([]byte("\n")))
}
