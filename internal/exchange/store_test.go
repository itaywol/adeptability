package exchange

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

// storeFactory builds a fresh Store for a conformance case. The fs variant
// returns its data dir so persistence-across-reopen can be exercised.
type storeFactory struct {
	name string
	open func(t *testing.T) (Store, string)
}

func storeFactories() []storeFactory {
	return []storeFactory{
		{name: "memory", open: func(t *testing.T) (Store, string) {
			s, err := NewDriverRegistry().Open("memory", "")
			require.NoError(t, err)
			return s, ""
		}},
		{name: "fs", open: func(t *testing.T) (Store, string) {
			dir := t.TempDir()
			s, err := NewDriverRegistry().Open("fs", dir)
			require.NoError(t, err)
			return s, dir
		}},
	}
}

func TestStore_Conformance(t *testing.T) {
	for _, f := range storeFactories() {
		t.Run(f.name, func(t *testing.T) {
			s, _ := f.open(t)

			t.Run("bootstrap set/get", func(t *testing.T) {
				h, err := s.BootstrapHash()
				require.NoError(t, err)
				require.Empty(t, h)
				require.NoError(t, s.SetBootstrapHash("abc"))
				h, err = s.BootstrapHash()
				require.NoError(t, err)
				require.Equal(t, "abc", h)
			})

			t.Run("users: create, lookup, dup, rotate", func(t *testing.T) {
				require.NoError(t, s.CreateUser(adept.ExchangeUser{Handle: "alice", TokenHash: "h1", Role: adept.ExchangeRoleMember}))
				require.ErrorIs(t, s.CreateUser(adept.ExchangeUser{Handle: "alice", TokenHash: "h2"}), adept.ErrExchangeHandleTaken)

				u, ok, err := s.UserByTokenHash("h1")
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, "alice", u.Handle)

				_, ok, err = s.UserByTokenHash("nope")
				require.NoError(t, err)
				require.False(t, ok)

				ru, ok, err := s.RotateUserToken("h1", "h1b")
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, "alice", ru.Handle)
				_, ok, _ = s.UserByTokenHash("h1")
				require.False(t, ok, "old token must stop resolving")
				_, ok, _ = s.UserByTokenHash("h1b")
				require.True(t, ok)
			})

			t.Run("items: create assigns id+status+timestamps", func(t *testing.T) {
				it, err := s.CreateItem(adept.ExchangeItem{Author: "alice", Title: "help", Body: "b", Assignees: []string{"bob"}})
				require.NoError(t, err)
				require.Equal(t, 1, it.ID)
				require.Equal(t, adept.ExchangeStatusAttention, it.Status)
				require.NotEmpty(t, it.CreatedAt)
				require.NotEmpty(t, it.UpdatedAt)

				got, err := s.GetItem(1)
				require.NoError(t, err)
				require.Equal(t, "help", got.Title)

				_, err = s.GetItem(999)
				require.ErrorIs(t, err, adept.ErrExchangeItemNotFound)
			})

			t.Run("comment auto-flips attention->in-progress", func(t *testing.T) {
				it, err := s.AddComment(1, adept.ExchangeComment{Author: "bob", Body: "try X"})
				require.NoError(t, err)
				require.Len(t, it.Comments, 1)
				require.Equal(t, adept.ExchangeStatusInProgress, it.Status)
				require.NotEmpty(t, it.Comments[0].CreatedAt)
			})

			t.Run("set status", func(t *testing.T) {
				it, err := s.SetStatus(1, adept.ExchangeStatusClosed)
				require.NoError(t, err)
				require.Equal(t, adept.ExchangeStatusClosed, it.Status)
			})

			t.Run("list: newest-first, mine + status filters", func(t *testing.T) {
				_, err := s.CreateItem(adept.ExchangeItem{Author: "carol", Title: "second"})
				require.NoError(t, err)

				all, err := s.ListItems(ListFilter{})
				require.NoError(t, err)
				require.Len(t, all, 2)
				require.Equal(t, 2, all[0].ID, "newest first")

				mine, err := s.ListItems(ListFilter{Mine: true, Handle: "bob"})
				require.NoError(t, err)
				require.Len(t, mine, 1, "bob is an assignee of item 1")
				require.Equal(t, 1, mine[0].ID)

				closed, err := s.ListItems(ListFilter{Status: adept.ExchangeStatusClosed})
				require.NoError(t, err)
				require.Len(t, closed, 1)
				require.Equal(t, 1, closed[0].ID)
			})
		})
	}
}

// TestFSStore_PersistsAcrossReopen proves the fs driver's write-through is
// durable: a second Open of the same dir sees prior state.
func TestFSStore_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	reg := NewDriverRegistry()

	s1, err := reg.Open("fs", dir)
	require.NoError(t, err)
	require.NoError(t, s1.SetBootstrapHash("boot"))
	require.NoError(t, s1.CreateUser(adept.ExchangeUser{Handle: "alice", TokenHash: "h1"}))
	it, err := s1.CreateItem(adept.ExchangeItem{Author: "alice", Title: "persisted"})
	require.NoError(t, err)
	_, err = s1.AddComment(it.ID, adept.ExchangeComment{Author: "bob", Body: "answer"})
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(dir, boardFileName))

	s2, err := reg.Open("fs", dir)
	require.NoError(t, err)
	h, err := s2.BootstrapHash()
	require.NoError(t, err)
	require.Equal(t, "boot", h)
	_, ok, _ := s2.UserByTokenHash("h1")
	require.True(t, ok)
	got, err := s2.GetItem(it.ID)
	require.NoError(t, err)
	require.Equal(t, adept.ExchangeStatusInProgress, got.Status)
	require.Len(t, got.Comments, 1)

	// New items continue the id sequence, not restart at 1.
	next, err := s2.CreateItem(adept.ExchangeItem{Author: "alice", Title: "second"})
	require.NoError(t, err)
	require.Equal(t, it.ID+1, next.ID)
}

func TestDriverRegistry_UnknownDriver(t *testing.T) {
	_, err := NewDriverRegistry().Open("postgres", "")
	require.ErrorIs(t, err, adept.ErrExchangeDriverUnknown)
}
