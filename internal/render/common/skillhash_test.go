package common_test

import (
	"testing"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func TestShortSkillHash_Deterministic(t *testing.T) {
	t.Parallel()
	s := &adept.Skill{
		ID:          "x",
		Description: "desc",
		Activation:  adept.ActivationAlways,
		Globs:       []string{"a", "b"},
		Body:        "body",
	}
	h1 := common.ShortSkillHash(s)
	h2 := common.ShortSkillHash(s)
	require.Equal(t, h1, h2)
	require.Len(t, h1, 8)
}

func TestShortSkillHash_GlobOrderInsensitive(t *testing.T) {
	t.Parallel()
	a := &adept.Skill{ID: "x", Globs: []string{"a", "b", "c"}, Body: "body"}
	b := &adept.Skill{ID: "x", Globs: []string{"c", "b", "a"}, Body: "body"}
	require.Equal(t, common.ShortSkillHash(a), common.ShortSkillHash(b))
}

func TestShortSkillHash_FileOrderInsensitive(t *testing.T) {
	t.Parallel()
	a := &adept.Skill{
		ID: "x",
		Files: []adept.SkillFile{
			{RelPath: "a.md", Bytes: []byte("aaa")},
			{RelPath: "b.md", Bytes: []byte("bbb")},
		},
	}
	b := &adept.Skill{
		ID: "x",
		Files: []adept.SkillFile{
			{RelPath: "b.md", Bytes: []byte("bbb")},
			{RelPath: "a.md", Bytes: []byte("aaa")},
		},
	}
	require.Equal(t, common.ShortSkillHash(a), common.ShortSkillHash(b))
}

func TestShortSkillHash_ChangeBodyChangesHash(t *testing.T) {
	t.Parallel()
	a := &adept.Skill{ID: "x", Body: "body one"}
	b := &adept.Skill{ID: "x", Body: "body two"}
	require.NotEqual(t, common.ShortSkillHash(a), common.ShortSkillHash(b))
}

func TestShortSkillHash_ChangeIDChangesHash(t *testing.T) {
	t.Parallel()
	a := &adept.Skill{ID: "x", Body: "body"}
	b := &adept.Skill{ID: "y", Body: "body"}
	require.NotEqual(t, common.ShortSkillHash(a), common.ShortSkillHash(b))
}

func TestShortSkillHash_ChangeFileContentChangesHash(t *testing.T) {
	t.Parallel()
	a := &adept.Skill{
		ID:    "x",
		Files: []adept.SkillFile{{RelPath: "a.md", Bytes: []byte("aaa")}},
	}
	b := &adept.Skill{
		ID:    "x",
		Files: []adept.SkillFile{{RelPath: "a.md", Bytes: []byte("bbb")}},
	}
	require.NotEqual(t, common.ShortSkillHash(a), common.ShortSkillHash(b))
}

func TestShortSkillHash_NilSkillStable(t *testing.T) {
	t.Parallel()
	require.Equal(t, common.ShortSkillHash(nil), common.ShortSkillHash(nil))
	require.Len(t, common.ShortSkillHash(nil), 8)
}

func TestShortSkillHash_MatchesNewMarkerRegex(t *testing.T) {
	t.Parallel()
	// 8 hex chars [a-f0-9].
	h := common.ShortSkillHash(&adept.Skill{ID: "x", Body: "y"})
	require.Len(t, h, 8)
	for _, c := range h {
		ok := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		require.True(t, ok, "non-hex char %q in hash %q", c, h)
	}
}
