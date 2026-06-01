package status

import (
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func TestResolver_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   Input
		want adept.Status
	}{
		{
			name: "nothing anywhere",
			in:   Input{ProjectHash: "", BaseHash: "", LibraryHash: ""},
			want: adept.StatusLocalOnly,
		},
		{
			name: "library only, project absent, no base",
			in:   Input{ProjectHash: "", BaseHash: "", LibraryHash: "h"},
			want: adept.StatusLibraryOnly,
		},
		{
			name: "library only, project absent, with base",
			in:   Input{ProjectHash: "", BaseHash: "h", LibraryHash: "h"},
			want: adept.StatusLibraryOnly,
		},
		{
			name: "project only, library absent, no base",
			in:   Input{ProjectHash: "h", BaseHash: "", LibraryHash: ""},
			want: adept.StatusLocalOnly,
		},
		{
			name: "project only, library absent, with base",
			in:   Input{ProjectHash: "h", BaseHash: "h", LibraryHash: ""},
			want: adept.StatusLocalOnly,
		},
		{
			name: "project and library, no base",
			in:   Input{ProjectHash: "h", BaseHash: "", LibraryHash: "h"},
			want: adept.StatusLocalOnly,
		},
		{
			name: "synced",
			in:   Input{ProjectHash: "h", BaseHash: "h", LibraryHash: "h"},
			want: adept.StatusSynced,
		},
		{
			name: "ahead - project changed",
			in:   Input{ProjectHash: "h2", BaseHash: "h1", LibraryHash: "h1"},
			want: adept.StatusAhead,
		},
		{
			name: "behind - library changed",
			in:   Input{ProjectHash: "h1", BaseHash: "h1", LibraryHash: "h2"},
			want: adept.StatusBehind,
		},
		{
			name: "diverged - both changed",
			in:   Input{ProjectHash: "hX", BaseHash: "h0", LibraryHash: "hY"},
			want: adept.StatusDiverged,
		},
		{
			name: "diverged - both changed to same new hash still diverged because each side moved",
			in:   Input{ProjectHash: "hZ", BaseHash: "h0", LibraryHash: "hZ"},
			want: adept.StatusDiverged,
		},
	}
	r := NewResolver()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, r.Resolve(tc.in))
		})
	}
}

func TestResolver_StatelessReusable(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	in := Input{ProjectHash: "h", BaseHash: "h", LibraryHash: "h"}
	a := r.Resolve(in)
	b := r.Resolve(in)
	require.Equal(t, a, b)
}
