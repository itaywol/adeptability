package exchange

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

func TestCredStore_SaveLoadDefault(t *testing.T) {
	cs := NewCredStore(t.TempDir())
	const server = "https://exchange.example.com"

	_, err := cs.Load(server)
	require.Error(t, err, "nothing saved yet")

	require.NoError(t, cs.Save(Creds{Server: server, Handle: "alice", Token: "tok"}))
	got, err := cs.Load(server)
	require.NoError(t, err)
	require.Equal(t, "alice", got.Handle)
	require.Equal(t, "tok", got.Token)
	require.Equal(t, server, cs.DefaultServer())
}

func TestCredStore_ResolveServerPrecedence(t *testing.T) {
	cs := NewCredStore(t.TempDir())

	_, err := cs.ResolveServer("")
	require.Error(t, err, "no flag, no env, no default")

	require.NoError(t, cs.Save(Creds{Server: "https://stored", Handle: "a", Token: "t"}))
	srv, err := cs.ResolveServer("")
	require.NoError(t, err)
	require.Equal(t, "https://stored", srv)

	srv, err = cs.ResolveServer("https://flag")
	require.NoError(t, err)
	require.Equal(t, "https://flag", srv, "flag wins over stored default")

	t.Setenv(adept.ExchangeServerEnvVar, "https://env")
	srv, err = cs.ResolveServer("")
	require.NoError(t, err)
	require.Equal(t, "https://env", srv, "env wins over stored default")
}

func TestCredStore_ResolveToken(t *testing.T) {
	cs := NewCredStore(t.TempDir())
	const server = "https://exchange.example.com"

	_, err := cs.ResolveToken(server)
	require.Error(t, err, "not registered")

	require.NoError(t, cs.Save(Creds{Server: server, Handle: "a", Token: "stored-tok"}))
	tok, err := cs.ResolveToken(server)
	require.NoError(t, err)
	require.Equal(t, "stored-tok", tok)

	t.Setenv(adept.ExchangeTokenEnvVar, "env-tok")
	tok, err = cs.ResolveToken(server)
	require.NoError(t, err)
	require.Equal(t, "env-tok", tok, "env overrides stored token")
}

func TestCredStore_RecommendationDismissal(t *testing.T) {
	cs := NewCredStore(t.TempDir())
	require.False(t, cs.RecommendationDismissed())

	require.NoError(t, cs.DismissRecommendation())
	require.True(t, cs.RecommendationDismissed())
	require.NoError(t, cs.DismissRecommendation(), "dismiss is idempotent")

	require.NoError(t, cs.UndismissRecommendation())
	require.False(t, cs.RecommendationDismissed())
	require.NoError(t, cs.UndismissRecommendation(), "undismiss is idempotent")
}

func TestCredStore_Registered(t *testing.T) {
	cs := NewCredStore(t.TempDir())
	require.False(t, cs.Registered(""))
	require.False(t, cs.Registered("https://exchange.example.com"))

	require.NoError(t, cs.Save(Creds{Server: "https://exchange.example.com", Handle: "a", Token: "t"}))
	require.True(t, cs.Registered("https://exchange.example.com"))
}
