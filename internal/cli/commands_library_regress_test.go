package cli

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// Regression: truncate must cut on rune boundaries, not bytes. A string
// whose (n-1)th byte falls inside a multi-byte rune previously produced
// invalid UTF-8 (a mangled glyph). The result must always be valid UTF-8
// and must never exceed n runes.
func TestTruncate_RuneBoundary_Regress(t *testing.T) {
	// 63 ASCII chars followed by a 4-byte emoji; n=64 means the cut lands
	// right where the emoji begins.
	s := strings.Repeat("a", 63) + "😀"
	out := truncate(s, 64)
	require.True(t, utf8.ValidString(out), "truncate must not split a multi-byte rune: %q", out)
	require.LessOrEqual(t, utf8.RuneCountInString(out), 64)

	// CJK near the boundary.
	cjk := strings.Repeat("世", 100)
	out2 := truncate(cjk, 10)
	require.True(t, utf8.ValidString(out2))
	require.Equal(t, 10, utf8.RuneCountInString(out2)) // 9 runes + ellipsis
	require.True(t, strings.HasSuffix(out2, "…"))

	// Rune-length guard: an all-ASCII string at exactly n is returned
	// unchanged (no false truncation from byte-vs-rune mismatch).
	require.Equal(t, "abc", truncate("abc", 3))

	// A multi-byte string with rune-count <= n is returned unchanged even
	// though its byte length exceeds n.
	short := "héllo" // 5 runes, 6 bytes
	require.Equal(t, short, truncate(short, 5))
}
