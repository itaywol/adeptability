package status

import (
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func ptr(e adept.LockEntry) *adept.LockEntry { return &e }

func TestResolver_Table(t *testing.T) {
	cases := []struct {
		name string
		in   Input
		want adept.Status
	}{
		{
			name: "nothing anywhere",
			in:   Input{ProjectHash: "", ProjectEntry: nil, LibraryEntry: nil},
			want: adept.StatusLocalOnly,
		},
		{
			name: "not on disk but library has it",
			in:   Input{ProjectHash: "", LibraryEntry: ptr(adept.LockEntry{Version: 1, Hash: "h"})},
			want: adept.StatusLibraryOnly,
		},
		{
			name: "on disk no project entry no library",
			in:   Input{ProjectHash: "h"},
			want: adept.StatusLocalOnly,
		},
		{
			name: "on disk project entry no library",
			in: Input{
				ProjectHash:  "h",
				ProjectEntry: ptr(adept.LockEntry{Version: 1, Hash: "h"}),
			},
			want: adept.StatusLocalOnly,
		},
		{
			name: "on disk library only, no project entry",
			in: Input{
				ProjectHash:  "h",
				LibraryEntry: ptr(adept.LockEntry{Version: 1, Hash: "h"}),
			},
			want: adept.StatusLocalOnly,
		},
		{
			name: "synced",
			in: Input{
				ProjectHash:  "h",
				ProjectEntry: ptr(adept.LockEntry{Version: 1, Hash: "h"}),
				LibraryEntry: ptr(adept.LockEntry{Version: 1, Hash: "h"}),
			},
			want: adept.StatusSynced,
		},
		{
			name: "ahead - hash changed on disk only",
			in: Input{
				ProjectHash:  "h2",
				ProjectEntry: ptr(adept.LockEntry{Version: 1, Hash: "h1"}),
				LibraryEntry: ptr(adept.LockEntry{Version: 1, Hash: "h1"}),
			},
			want: adept.StatusAhead,
		},
		{
			name: "behind - library bumped version",
			in: Input{
				ProjectHash:  "h1",
				ProjectEntry: ptr(adept.LockEntry{Version: 1, Hash: "h1"}),
				LibraryEntry: ptr(adept.LockEntry{Version: 2, Hash: "h2"}),
			},
			want: adept.StatusBehind,
		},
		{
			name: "behind - library hash changed same version",
			in: Input{
				ProjectHash:  "h1",
				ProjectEntry: ptr(adept.LockEntry{Version: 1, Hash: "h1"}),
				LibraryEntry: ptr(adept.LockEntry{Version: 1, Hash: "h2"}),
			},
			want: adept.StatusBehind,
		},
		{
			name: "diverged - both changed",
			in: Input{
				ProjectHash:  "hX",
				ProjectEntry: ptr(adept.LockEntry{Version: 1, Hash: "h1"}),
				LibraryEntry: ptr(adept.LockEntry{Version: 2, Hash: "h2"}),
			},
			want: adept.StatusDiverged,
		},
	}
	r := NewResolver()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, r.Resolve(tc.in))
		})
	}
}

func TestResolver_StatelessReusable(t *testing.T) {
	r := NewResolver()
	a := r.Resolve(Input{ProjectHash: "h",
		ProjectEntry: ptr(adept.LockEntry{Version: 1, Hash: "h"}),
		LibraryEntry: ptr(adept.LockEntry{Version: 1, Hash: "h"}),
	})
	b := r.Resolve(Input{ProjectHash: "h",
		ProjectEntry: ptr(adept.LockEntry{Version: 1, Hash: "h"}),
		LibraryEntry: ptr(adept.LockEntry{Version: 1, Hash: "h"}),
	})
	require.Equal(t, a, b)
}
