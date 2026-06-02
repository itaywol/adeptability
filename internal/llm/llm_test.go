package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	name  string
	avail error
}

func (f *fakeProvider) Name() string                                          { return f.name }
func (f *fakeProvider) DefaultModel() string                                  { return "test-model" }
func (f *fakeProvider) Available(context.Context) error                       { return f.avail }
func (f *fakeProvider) Evaluate(context.Context, Request) (Response, error)   { return Response{Text: "ok"}, nil }

func TestRegistry_Get_KnownProvider(t *testing.T) {
	reg := NewRegistry(&fakeProvider{name: "alpha"}, &fakeProvider{name: "beta"})
	p, err := reg.Get("alpha")
	require.NoError(t, err)
	require.Equal(t, "alpha", p.Name())
}

func TestRegistry_Get_UnknownReturnsTypedError(t *testing.T) {
	reg := NewRegistry(&fakeProvider{name: "alpha"})
	_, err := reg.Get("zeta")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrProviderUnknown))
}

func TestRegistry_List_SortedNames(t *testing.T) {
	reg := NewRegistry(&fakeProvider{name: "zeta"}, &fakeProvider{name: "alpha"})
	require.Equal(t, []string{"alpha", "zeta"}, reg.List())
}

func TestProvider_AvailableSurfaceError(t *testing.T) {
	p := &fakeProvider{name: "x", avail: errors.New("no key")}
	require.Error(t, p.Available(context.Background()))
}
