package merge

import (
	"bytes"
)

// diff3Merge runs a line-based 3-way merge between ours / base / theirs.
// It returns the merged bytes and whether the result contains conflict
// markers. The algorithm is the textbook diff3:
//
//  1. Compute LCS of base↔ours and base↔theirs.
//  2. Iterate over base lines, building "sync points" — line indexes
//     where the same base line matches in BOTH ours and theirs at the
//     same relative position. Sync points partition the inputs into
//     alternating stable/unstable hunks.
//  3. Emit each stable hunk verbatim from ours.
//  4. For each unstable hunk: choose ours-only, theirs-only, identical,
//     or emit conflict markers depending on which side(s) changed.
//
// Determinism: identical (ours, base, theirs) inputs always produce
// byte-identical outputs because:
//   - The LCS tables are deterministic (dynamic programming).
//   - Sync-point selection prefers earlier matches (left-to-right walk).
//   - Conflict marker order is fixed: ours / base / theirs / end.
//   - We never iterate maps when emitting bytes.
func diff3Merge(ours, base, theirs []string, opts Options) (out []byte, conflicted bool) {
	chunks := buildChunks(ours, base, theirs)
	var buf bytes.Buffer
	for _, c := range chunks {
		if c.stable {
			writeLines(&buf, ours[c.oursStart:c.oursEnd])
			continue
		}
		o := ours[c.oursStart:c.oursEnd]
		b := base[c.baseStart:c.baseEnd]
		t := theirs[c.theirsStart:c.theirsEnd]
		oEqB := linesEqual(o, b)
		tEqB := linesEqual(t, b)
		oEqT := linesEqual(o, t)
		switch {
		case oEqB && tEqB:
			// Nobody changed; take ours (== base == theirs).
			writeLines(&buf, o)
		case oEqB && !tEqB:
			// Only theirs changed.
			writeLines(&buf, t)
		case tEqB && !oEqB:
			// Only ours changed.
			writeLines(&buf, o)
		case oEqT:
			// Both sides made the same change.
			writeLines(&buf, o)
		default:
			// Real conflict.
			conflicted = true
			writeLn(&buf, opts.MarkerOurs)
			writeLines(&buf, o)
			ensureTrailingNewline(&buf)
			writeLn(&buf, opts.MarkerBase)
			writeLines(&buf, b)
			ensureTrailingNewline(&buf)
			writeLn(&buf, opts.MarkerTheirs)
			writeLines(&buf, t)
			ensureTrailingNewline(&buf)
			writeLn(&buf, opts.MarkerEnd)
		}
	}
	return buf.Bytes(), conflicted
}

// chunk describes a contiguous slice of (ours, base, theirs) line
// indexes. Stable chunks span exactly one sync point per side (or the
// region preceding/following all sync points if empty); unstable chunks
// are the gaps between sync points where one or both sides diverged.
type chunk struct {
	stable                 bool
	oursStart, oursEnd     int
	baseStart, baseEnd     int
	theirsStart, theirsEnd int
}

// buildChunks identifies sync points and partitions the inputs.
//
// A sync point is a triple (i, j, k) such that ours[i] == base[j] ==
// theirs[k] AND each of ours[i]/base[j]/theirs[k] sits on an LCS path
// between base↔ours and base↔theirs respectively. We greedily walk the
// base lines that are present in BOTH LCS sequences (base↔ours and
// base↔theirs) and emit each as a single-line stable chunk; everything
// between successive sync points is an unstable chunk to be resolved
// by the caller in diff3Merge.
func buildChunks(ours, base, theirs []string) []chunk {
	matchOurs := lcsMatches(base, ours)
	matchTheirs := lcsMatches(base, theirs)
	type sync struct{ o, b, t int }
	var syncs []sync
	oursMin, theirsMin := 0, 0
	for j := 0; j < len(base); j++ {
		oi := matchOurs[j]
		ti := matchTheirs[j]
		if oi < 0 || ti < 0 {
			continue
		}
		if oi < oursMin || ti < theirsMin {
			// LCS table may emit an out-of-order match at this
			// position because we picked an earlier match for a
			// previous base line. Skip — emitting it would force a
			// non-monotonic chunk.
			continue
		}
		syncs = append(syncs, sync{o: oi, b: j, t: ti})
		oursMin = oi + 1
		theirsMin = ti + 1
	}

	// Each sync contributes at most one unstable chunk plus one stable
	// chunk, and the trailing region may add one more.
	out := make([]chunk, 0, len(syncs)*2+1)
	oursPos, basePos, theirsPos := 0, 0, 0
	for _, s := range syncs {
		if oursPos < s.o || basePos < s.b || theirsPos < s.t {
			out = append(out, chunk{
				oursStart:   oursPos,
				oursEnd:     s.o,
				baseStart:   basePos,
				baseEnd:     s.b,
				theirsStart: theirsPos,
				theirsEnd:   s.t,
			})
		}
		out = append(out, chunk{
			stable:      true,
			oursStart:   s.o,
			oursEnd:     s.o + 1,
			baseStart:   s.b,
			baseEnd:     s.b + 1,
			theirsStart: s.t,
			theirsEnd:   s.t + 1,
		})
		oursPos = s.o + 1
		basePos = s.b + 1
		theirsPos = s.t + 1
	}
	// Trailing unstable region (or trailing stable nothing).
	if oursPos < len(ours) || basePos < len(base) || theirsPos < len(theirs) {
		out = append(out, chunk{
			oursStart:   oursPos,
			oursEnd:     len(ours),
			baseStart:   basePos,
			baseEnd:     len(base),
			theirsStart: theirsPos,
			theirsEnd:   len(theirs),
		})
	}
	return out
}

// lcsMatches returns, for each base line index j, the index in other
// that this base line matches to in the LCS of (base, other), or -1 if
// the line is not part of the LCS. The standard Hunt-McIlroy backtrace
// is used: O(n*m) time and space, which is fine for the canonical files
// the merge sees (single SKILL.md plus a handful of sidecar scripts).
func lcsMatches(base, other []string) []int {
	n := len(base)
	m := len(other)
	matches := make([]int, n)
	for i := range matches {
		matches[i] = -1
	}
	if n == 0 || m == 0 {
		return matches
	}
	// dp[i][j] = LCS length of base[:i] and other[:j]
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		bi := base[i-1]
		for j := 1; j <= m; j++ {
			switch {
			case bi == other[j-1]:
				dp[i][j] = dp[i-1][j-1] + 1
			case dp[i-1][j] >= dp[i][j-1]:
				dp[i][j] = dp[i-1][j]
			default:
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	// Backtrace, preferring the leftmost match for determinism.
	i, j := n, m
	for i > 0 && j > 0 {
		switch {
		case base[i-1] == other[j-1]:
			matches[i-1] = j - 1
			i--
			j--
		case dp[i-1][j] >= dp[i][j-1]:
			i--
		default:
			j--
		}
	}
	return matches
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeLines(buf *bytes.Buffer, lines []string) {
	for _, l := range lines {
		buf.WriteString(l)
	}
}

func writeLn(buf *bytes.Buffer, s string) {
	buf.WriteString(s)
	if len(s) == 0 || s[len(s)-1] != '\n' {
		buf.WriteByte('\n')
	}
}

func ensureTrailingNewline(buf *bytes.Buffer) {
	b := buf.Bytes()
	if len(b) == 0 || b[len(b)-1] == '\n' {
		return
	}
	buf.WriteByte('\n')
}
