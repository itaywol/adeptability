package common_test

import (
	"strings"
	"testing"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/stretchr/testify/require"
)

func TestFrontmatterBuilder_Empty(t *testing.T) {
	t.Parallel()
	got, err := common.NewFrontmatterBuilder().Build(nil)
	require.NoError(t, err)
	require.Equal(t, "", got)
}

func TestFrontmatterBuilder_StableOrder(t *testing.T) {
	t.Parallel()
	got, err := common.NewFrontmatterBuilder().Build([]common.Field{
		{Key: "name", Value: "demo"},
		{Key: "description", Value: "First skill"},
		{Key: "allowed-tools", Value: []string{"Read", "Write"}},
		{Key: "disable-model-invocation", Value: true},
	})
	require.NoError(t, err)
	expected := strings.Join([]string{
		"---",
		"name: demo",
		"description: First skill",
		"allowed-tools: [Read, Write]",
		"disable-model-invocation: true",
		"---",
		"",
	}, "\n")
	require.Equal(t, expected, got)
}

func TestFrontmatterBuilder_QuotedString(t *testing.T) {
	t.Parallel()
	got, err := common.NewFrontmatterBuilder().Build([]common.Field{
		{Key: "description", Value: "calls: are tricky", Quote: true},
	})
	require.NoError(t, err)
	require.Contains(t, got, `description: "calls: are tricky"`)
}

func TestFrontmatterBuilder_GlobsArray(t *testing.T) {
	t.Parallel()
	got, err := common.NewFrontmatterBuilder().Build([]common.Field{
		{Key: "description", Value: "Glob rule"},
		{Key: "globs", Value: []string{"**/*.ts", "**/*.tsx"}},
		{Key: "alwaysApply", Value: false},
	})
	require.NoError(t, err)
	// yaml.v3 quotes globs that begin with '*' to keep them as strings.
	require.Contains(t, got, "globs: ['**/*.ts', '**/*.tsx']")
	require.Contains(t, got, "alwaysApply: false")
}

func TestFrontmatterBuilder_EmptyKey(t *testing.T) {
	t.Parallel()
	_, err := common.NewFrontmatterBuilder().Build([]common.Field{
		{Key: "", Value: "x"},
	})
	require.Error(t, err)
}

func TestFrontmatterBuilder_StructuredValuesEncode(t *testing.T) {
	t.Parallel()
	// Structured values (maps/slices) now encode via yaml so per-harness
	// override blocks can carry them.
	out, err := common.NewFrontmatterBuilder().Build([]common.Field{
		{Key: "k", Value: map[string]any{"a": "b"}},
	})
	require.NoError(t, err)
	require.Contains(t, out, "a: b")
}

func TestFrontmatterBuilder_UnsupportedType(t *testing.T) {
	t.Parallel()
	// A channel cannot be yaml-encoded, so it still surfaces an error.
	_, err := common.NewFrontmatterBuilder().Build([]common.Field{
		{Key: "k", Value: make(chan int)},
	})
	require.Error(t, err)
}
