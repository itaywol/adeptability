package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

// mockAdapter is the minimum implementation of adept.HarnessAdapter we need
// to exercise the registry + orchestrator without pulling in the per-harness
// renderer packages.
type mockAdapter struct {
	spec   adept.HarnessSpec
	render adept.Renderer
	agg    func(context.Context, []adept.RenderOutput, int) ([]adept.RenderOutput, error)
	detect func(string) (bool, error)
	valid  func(string, []adept.RenderOutput) (adept.DriftReport, error)
}

func (m *mockAdapter) Spec() adept.HarnessSpec   { return m.spec }
func (m *mockAdapter) Renderer() adept.Renderer  { return m.render }
func (m *mockAdapter) Aggregate(ctx context.Context, parts []adept.RenderOutput, budget int) ([]adept.RenderOutput, error) {
	if m.agg == nil {
		return parts, nil
	}
	return m.agg(ctx, parts, budget)
}
func (m *mockAdapter) Detect(root string) (bool, error) {
	if m.detect == nil {
		return false, nil
	}
	return m.detect(root)
}
func (m *mockAdapter) Validate(root string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	if m.valid == nil {
		return adept.DriftReport{}, nil
	}
	return m.valid(root, expected)
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	a := &mockAdapter{spec: adept.HarnessSpec{ID: "alpha", Kind: adept.KindPerSkill}}
	require.NoError(t, reg.Register(a))
	got, err := reg.Get("alpha")
	require.NoError(t, err)
	require.Same(t, a, got)
}

func TestRegistry_DuplicateRegisterFails(t *testing.T) {
	reg := NewRegistry()
	a := &mockAdapter{spec: adept.HarnessSpec{ID: "alpha", Kind: adept.KindPerSkill}}
	require.NoError(t, reg.Register(a))
	err := reg.Register(a)
	require.ErrorIs(t, err, adept.ErrAdapterInvalid)
}

func TestRegistry_NilAdapterRejected(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(nil)
	require.ErrorIs(t, err, adept.ErrAdapterInvalid)
}

func TestRegistry_EmptyIDRejected(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(&mockAdapter{spec: adept.HarnessSpec{Kind: adept.KindPerSkill}})
	require.ErrorIs(t, err, adept.ErrAdapterInvalid)
}

func TestRegistry_GetUnknown(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Get("missing")
	require.ErrorIs(t, err, adept.ErrHarnessUnknown)
}

func TestRegistry_ListSorted(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&mockAdapter{spec: adept.HarnessSpec{ID: "z"}}))
	require.NoError(t, reg.Register(&mockAdapter{spec: adept.HarnessSpec{ID: "a"}}))
	require.NoError(t, reg.Register(&mockAdapter{spec: adept.HarnessSpec{ID: "m"}}))
	all := reg.List()
	require.Len(t, all, 3)
	require.Equal(t, "a", all[0].Spec().ID)
	require.Equal(t, "m", all[1].Spec().ID)
	require.Equal(t, "z", all[2].Spec().ID)
}
