package exchange

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// tokenBytes is the entropy per issued token (256 bits).
const tokenBytes = 32

// generateToken returns a fresh URL-safe bearer token. Tokens are opaque;
// only their hash is ever persisted server-side.
func generateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("exchange: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex sha256 of a token, the form stored in the board.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// tokenMatches compares a presented token against a stored hash in constant
// time, so a timing side-channel can't leak the hash.
func tokenMatches(token, storedHash string) bool {
	got := hashToken(token)
	return subtle.ConstantTimeCompare([]byte(got), []byte(storedHash)) == 1
}

// EnsureBootstrap returns the bootstrap token for a board, generating and
// persisting one when none exists or force is true. The raw token is returned
// ONLY when freshly minted — it is never recoverable afterwards, so callers
// must surface it to the operator immediately. When a bootstrap token already
// exists and force is false, it returns ("", nil).
func EnsureBootstrap(store Store, force bool) (string, error) {
	cur, err := store.BootstrapHash()
	if err != nil {
		return "", err
	}
	if cur != "" && !force {
		return "", nil
	}
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	if err := store.SetBootstrapHash(hashToken(token)); err != nil {
		return "", err
	}
	return token, nil
}
