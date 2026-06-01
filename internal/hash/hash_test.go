package hash

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, rel string, data []byte) {
	t.Helper()
	full := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, data, 0o644))
}

func TestHash_SameDirSameHash(t *testing.T) {
	h := NewHasher()
	dir := t.TempDir()
	writeFile(t, dir, "SKILL.md", []byte("body\n"))
	writeFile(t, dir, "scripts/run.sh", []byte("#!/bin/sh\necho hi\n"))

	a, err := h.HashSkillDir(dir)
	require.NoError(t, err)
	b, err := h.HashSkillDir(dir)
	require.NoError(t, err)
	require.Equal(t, a, b)
}

func TestHash_LineEndingNormalization(t *testing.T) {
	h := NewHasher()
	lf := t.TempDir()
	crlf := t.TempDir()
	writeFile(t, lf, "SKILL.md", []byte("line1\nline2\n"))
	writeFile(t, crlf, "SKILL.md", []byte("line1\r\nline2\r\n"))

	a, err := h.HashSkillDir(lf)
	require.NoError(t, err)
	b, err := h.HashSkillDir(crlf)
	require.NoError(t, err)
	require.Equal(t, a, b, "CRLF and LF must hash identically")
}

func TestHash_OrderIndependence(t *testing.T) {
	h := NewHasher()
	d1 := t.TempDir()
	writeFile(t, d1, "a.txt", []byte("alpha"))
	writeFile(t, d1, "z.txt", []byte("zulu"))

	d2 := t.TempDir()
	// Same content but write order reversed; filesystem may return them in
	// either order. Hash must be identical because we sort.
	writeFile(t, d2, "z.txt", []byte("zulu"))
	writeFile(t, d2, "a.txt", []byte("alpha"))

	a, err := h.HashSkillDir(d1)
	require.NoError(t, err)
	b, err := h.HashSkillDir(d2)
	require.NoError(t, err)
	require.Equal(t, a, b)
}

func TestHash_OneByteChangeDiffers(t *testing.T) {
	h := NewHasher()
	d1 := t.TempDir()
	writeFile(t, d1, "SKILL.md", []byte("hello"))
	d2 := t.TempDir()
	writeFile(t, d2, "SKILL.md", []byte("hellp"))

	a, err := h.HashSkillDir(d1)
	require.NoError(t, err)
	b, err := h.HashSkillDir(d2)
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestHash_IgnoreFiltersFiles(t *testing.T) {
	h := NewHasher()
	d1 := t.TempDir()
	writeFile(t, d1, "SKILL.md", []byte("body"))
	writeFile(t, d1, "build.log", []byte("log v1"))
	writeFile(t, d1, ".adeptignore", []byte("**/*.log\n"))
	a, err := h.HashSkillDir(d1)
	require.NoError(t, err)

	// Modify the ignored file: hash should not change.
	writeFile(t, d1, "build.log", []byte("log v2 - different"))
	b, err := h.HashSkillDir(d1)
	require.NoError(t, err)
	require.Equal(t, a, b, "ignored file changes must not affect hash")
}

func TestHash_DotDirsExcluded(t *testing.T) {
	h := NewHasher()
	d1 := t.TempDir()
	writeFile(t, d1, "SKILL.md", []byte("body"))
	writeFile(t, d1, ".git/HEAD", []byte("ref: refs/heads/main"))
	a, err := h.HashSkillDir(d1)
	require.NoError(t, err)
	// Mutate dotted dir contents
	writeFile(t, d1, ".git/HEAD", []byte("ref: refs/heads/dev"))
	b, err := h.HashSkillDir(d1)
	require.NoError(t, err)
	require.Equal(t, a, b)
}

func TestHash_LockfileExcluded(t *testing.T) {
	h := NewHasher()
	d := t.TempDir()
	writeFile(t, d, "SKILL.md", []byte("body"))
	a, err := h.HashSkillDir(d)
	require.NoError(t, err)
	writeFile(t, d, adept.LockFileName, []byte(`{"schema":2}`))
	b, err := h.HashSkillDir(d)
	require.NoError(t, err)
	require.Equal(t, a, b)
}

func TestHash_NewFileChangesHash(t *testing.T) {
	h := NewHasher()
	d := t.TempDir()
	writeFile(t, d, "SKILL.md", []byte("body"))
	a, err := h.HashSkillDir(d)
	require.NoError(t, err)
	writeFile(t, d, "extra.txt", []byte("new content"))
	b, err := h.HashSkillDir(d)
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestHash_PrefixedAndHexLength(t *testing.T) {
	h := NewHasher()
	d := t.TempDir()
	writeFile(t, d, "SKILL.md", []byte("body"))
	got, err := h.HashSkillDir(d)
	require.NoError(t, err)
	require.Len(t, got, len("sha256:")+64)
	require.Equal(t, "sha256:", got[:7])
}

func TestHash_NotADirectory(t *testing.T) {
	h := NewHasher()
	d := t.TempDir()
	f := filepath.Join(d, "file")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))
	_, err := h.HashSkillDir(f)
	require.Error(t, err)
}

func TestHashSkill_Deterministic(t *testing.T) {
	h := NewHasher()
	s := &adept.Skill{
		ID:          "ok",
		Version:     1,
		Description: "x",
		Activation:  adept.ActivationAgent,
		Body:        "body\n",
	}
	a, err := h.HashSkill(s)
	require.NoError(t, err)
	b, err := h.HashSkill(s)
	require.NoError(t, err)
	require.Equal(t, a, b)
}

func TestHashSkill_DifferentBodyDiffers(t *testing.T) {
	h := NewHasher()
	s1 := &adept.Skill{ID: "ok", Version: 1, Description: "x", Body: "body1"}
	s2 := &adept.Skill{ID: "ok", Version: 1, Description: "x", Body: "body2"}
	a, err := h.HashSkill(s1)
	require.NoError(t, err)
	b, err := h.HashSkill(s2)
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestHashSkill_CRLFEquivalentToLF(t *testing.T) {
	h := NewHasher()
	s1 := &adept.Skill{ID: "ok", Version: 1, Description: "x", Body: "a\nb\n"}
	s2 := &adept.Skill{ID: "ok", Version: 1, Description: "x", Body: "a\r\nb\r\n"}
	a, err := h.HashSkill(s1)
	require.NoError(t, err)
	b, err := h.HashSkill(s2)
	require.NoError(t, err)
	require.Equal(t, a, b)
}

func TestHashSkill_NilSkill(t *testing.T) {
	h := NewHasher()
	_, err := h.HashSkill(nil)
	require.Error(t, err)
}
