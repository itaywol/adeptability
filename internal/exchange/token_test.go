package exchange

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToken_GenerateUniqueAndHashStable(t *testing.T) {
	a, err := generateToken()
	require.NoError(t, err)
	b, err := generateToken()
	require.NoError(t, err)
	require.NotEqual(t, a, b, "tokens must be unique")
	require.NotEmpty(t, a)

	require.Equal(t, hashToken(a), hashToken(a), "hash is deterministic")
	require.NotEqual(t, hashToken(a), hashToken(b))
}

func TestToken_Matches(t *testing.T) {
	tok, err := generateToken()
	require.NoError(t, err)
	h := hashToken(tok)
	require.True(t, tokenMatches(tok, h))
	require.False(t, tokenMatches("wrong", h))
	require.False(t, tokenMatches(tok, "deadbeef"))
}

func TestEnsureBootstrap(t *testing.T) {
	s, err := NewDriverRegistry().Open("memory", "")
	require.NoError(t, err)

	first, err := EnsureBootstrap(s, false)
	require.NoError(t, err)
	require.NotEmpty(t, first, "first call mints a token")

	again, err := EnsureBootstrap(s, false)
	require.NoError(t, err)
	require.Empty(t, again, "existing token is not re-minted without force")

	// The minted token authenticates registration.
	h, err := s.BootstrapHash()
	require.NoError(t, err)
	require.True(t, tokenMatches(first, h))

	rotated, err := EnsureBootstrap(s, true)
	require.NoError(t, err)
	require.NotEmpty(t, rotated)
	require.NotEqual(t, first, rotated)
	require.False(t, tokenMatches(first, mustBootHash(t, s)), "old bootstrap token invalidated")
	require.True(t, tokenMatches(rotated, mustBootHash(t, s)))
}

func mustBootHash(t *testing.T, s Store) string {
	t.Helper()
	h, err := s.BootstrapHash()
	require.NoError(t, err)
	return h
}
