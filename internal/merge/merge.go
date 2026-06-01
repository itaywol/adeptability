// Package merge implements a deterministic file-by-file 3-way merge for
// diverged skills. Given three on-disk directories — base (last common
// ancestor), ours (current project state) and theirs (current library
// state) — Merge walks the union of regular files, applies per-file
// rules (added-on-one-side, deleted-on-one-side, identical change, etc.)
// and falls back to a line-based diff3 algorithm when both sides
// modified the same text file.
//
// The package is fully self-contained: no external diff3 library, no
// dependency on os/exec, no globals. Determinism is enforced by sorting
// file paths lexicographically, hashing file contents with SHA-256 for
// equality (so binary files compare cheaply), and emitting diff3
// conflict markers in a fixed format. Identical inputs always yield
// byte-identical outputs.
package merge

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Conflict describes a single unresolved conflict within a merge run.
// Path is the relative file path inside the skill directory (forward
// slashes for cross-OS stability). HasConflict is always true for
// values returned in Result.Conflicts; the field exists so callers can
// reuse the type as a flag on per-file metadata if they want.
type Conflict struct {
	Path        string `json:"path"`
	HasConflict bool   `json:"hasConflict"`
}

// ResultFile is one merged file in a Result. Bytes is the byte-stable
// merged content, ready to be written via fsutil.AtomicWrite. Conflict
// is true when Bytes contains diff3 conflict markers or when this
// entry represents a delete-vs-modify conflict (see Result.Conflicts).
type ResultFile struct {
	RelPath  string      `json:"relPath"`
	Bytes    []byte      `json:"-"`
	Mode     os.FileMode `json:"mode"`
	Conflict bool        `json:"conflict"`
	// Deleted is true when the merge decided this path should be
	// removed from the project canonical tree (file existed in base
	// + ours but missing in theirs, with ours unchanged from base, or
	// vice versa). Bytes is nil for deletes.
	Deleted bool `json:"deleted"`
}

// Result is the output of one Merge call. Files is the per-relative-path
// outcome (write or delete). Conflicts is the subset of Files where
// human resolution is required.
type Result struct {
	Files     []ResultFile `json:"files"`
	Conflicts []Conflict   `json:"conflicts"`
}

// Merger runs a 3-way merge between three directories. Implementations
// must be safe for concurrent use across distinct calls (no shared
// mutable state) and must produce deterministic output.
type Merger interface {
	Merge(baseDir, oursDir, theirsDir string) (Result, error)
}

// Options tweaks merge behavior. The zero value is the documented
// default: line-based diff3 with stable conflict markers, no path
// filtering, treat files with NUL bytes as binary.
type Options struct {
	// BinaryProbeBytes caps how many leading bytes of a file are
	// scanned for NUL bytes when classifying it as binary. Zero falls
	// back to a 4 KiB probe.
	BinaryProbeBytes int
	// MarkerOurs / MarkerBase / MarkerTheirs / MarkerEnd override the
	// default conflict marker labels. Empty strings keep the defaults.
	MarkerOurs   string
	MarkerBase   string
	MarkerTheirs string
	MarkerEnd    string
}

const (
	defaultBinaryProbeBytes = 4096
	defaultMarkerOurs       = "<<<<<<< ours"
	defaultMarkerBase       = "||||||| base"
	defaultMarkerTheirs     = "======="
	defaultMarkerEnd        = ">>>>>>> theirs"
)

// NewMerger constructs a Merger using the zero-value Options. Callers
// that need to customize behavior should use NewMergerWithOptions.
func NewMerger() Merger {
	return NewMergerWithOptions(Options{})
}

// NewMergerWithOptions constructs a Merger with explicit options.
// Unset fields fall back to the documented defaults.
func NewMergerWithOptions(o Options) Merger {
	if o.BinaryProbeBytes <= 0 {
		o.BinaryProbeBytes = defaultBinaryProbeBytes
	}
	if o.MarkerOurs == "" {
		o.MarkerOurs = defaultMarkerOurs
	}
	if o.MarkerBase == "" {
		o.MarkerBase = defaultMarkerBase
	}
	if o.MarkerTheirs == "" {
		o.MarkerTheirs = defaultMarkerTheirs
	}
	if o.MarkerEnd == "" {
		o.MarkerEnd = defaultMarkerEnd
	}
	return &merger{opts: o}
}

type merger struct {
	opts Options
}

// fileEntry is a lightweight handle on a regular file within one of the
// three input directories. Bytes is loaded lazily by load().
type fileEntry struct {
	path    string // absolute on-disk path
	relPath string // forward-slash relative path inside the side root
	mode    os.FileMode
	loaded  bool
	bytes   []byte
}

func (e *fileEntry) load() ([]byte, error) {
	if e == nil {
		return nil, nil
	}
	if e.loaded {
		return e.bytes, nil
	}
	data, err := os.ReadFile(e.path)
	if err != nil {
		return nil, fmt.Errorf("merge: read %s: %w", e.path, err)
	}
	e.bytes = data
	e.loaded = true
	return data, nil
}

// sideIndex maps a side's relative paths -> fileEntry. Walks are
// path-sorted so iteration order over the union below is stable.
type sideIndex map[string]*fileEntry

func (m *merger) Merge(baseDir, oursDir, theirsDir string) (Result, error) {
	if oursDir == "" || theirsDir == "" {
		return Result{}, errors.New("merge: ours and theirs are required")
	}
	baseIdx, err := indexDir(baseDir)
	if err != nil {
		return Result{}, err
	}
	oursIdx, err := indexDir(oursDir)
	if err != nil {
		return Result{}, err
	}
	theirsIdx, err := indexDir(theirsDir)
	if err != nil {
		return Result{}, err
	}

	keys := unionKeys(baseIdx, oursIdx, theirsIdx)
	out := Result{Files: make([]ResultFile, 0, len(keys))}

	for _, rel := range keys {
		bEntry := baseIdx[rel]
		oEntry := oursIdx[rel]
		tEntry := theirsIdx[rel]

		file, conflicted, err := m.mergeOne(rel, bEntry, oEntry, tEntry)
		if err != nil {
			return Result{}, err
		}
		out.Files = append(out.Files, file)
		if conflicted {
			out.Conflicts = append(out.Conflicts, Conflict{Path: rel, HasConflict: true})
		}
	}

	// Defensive: keep the output deterministic even if walking order
	// ever drifts on some filesystem.
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].RelPath < out.Files[j].RelPath })
	sort.Slice(out.Conflicts, func(i, j int) bool { return out.Conflicts[i].Path < out.Conflicts[j].Path })
	return out, nil
}

// mergeOne implements the per-file decision table from the package
// docstring. Returns the resolved ResultFile and whether it is
// conflicted.
func (m *merger) mergeOne(rel string, base, ours, theirs *fileEntry) (ResultFile, bool, error) {
	switch {
	// Present in all three sides.
	case base != nil && ours != nil && theirs != nil:
		return m.mergeAllThree(rel, base, ours, theirs)

	// Not in base, in both ours and theirs (added in parallel).
	case base == nil && ours != nil && theirs != nil:
		return m.mergeBothAdded(rel, ours, theirs)

	// Added only on one side.
	case base == nil && ours != nil && theirs == nil:
		data, err := ours.load()
		if err != nil {
			return ResultFile{}, false, err
		}
		return ResultFile{RelPath: rel, Bytes: data, Mode: ours.mode}, false, nil
	case base == nil && theirs != nil && ours == nil:
		data, err := theirs.load()
		if err != nil {
			return ResultFile{}, false, err
		}
		return ResultFile{RelPath: rel, Bytes: data, Mode: theirs.mode}, false, nil

	// Deleted in both sides relative to base: drop it.
	case base != nil && ours == nil && theirs == nil:
		return ResultFile{RelPath: rel, Mode: base.mode, Deleted: true}, false, nil

	// Deleted on one side; the other unchanged from base → delete.
	// Deleted on one side; the other changed → conflict.
	case base != nil && ours == nil && theirs != nil:
		same, err := sameContent(base, theirs)
		if err != nil {
			return ResultFile{}, false, err
		}
		if same {
			return ResultFile{RelPath: rel, Mode: base.mode, Deleted: true}, false, nil
		}
		// We deleted, they changed → conflict, keep their version
		// inside the conflict envelope.
		theirBytes, err := theirs.load()
		if err != nil {
			return ResultFile{}, false, err
		}
		body := m.renderDeleteModifyConflict(rel, "ours", "theirs", nil, theirBytes)
		return ResultFile{RelPath: rel, Bytes: body, Mode: theirs.mode, Conflict: true}, true, nil
	case base != nil && theirs == nil && ours != nil:
		same, err := sameContent(base, ours)
		if err != nil {
			return ResultFile{}, false, err
		}
		if same {
			return ResultFile{RelPath: rel, Mode: base.mode, Deleted: true}, false, nil
		}
		ourBytes, err := ours.load()
		if err != nil {
			return ResultFile{}, false, err
		}
		body := m.renderDeleteModifyConflict(rel, "theirs", "ours", nil, ourBytes)
		return ResultFile{RelPath: rel, Bytes: body, Mode: ours.mode, Conflict: true}, true, nil
	}

	return ResultFile{}, false, fmt.Errorf("merge: unreachable state for %q", rel)
}

func (m *merger) mergeAllThree(rel string, base, ours, theirs *fileEntry) (ResultFile, bool, error) {
	oursEqBase, err := sameContent(ours, base)
	if err != nil {
		return ResultFile{}, false, err
	}
	theirsEqBase, err := sameContent(theirs, base)
	if err != nil {
		return ResultFile{}, false, err
	}
	oursEqTheirs, err := sameContent(ours, theirs)
	if err != nil {
		return ResultFile{}, false, err
	}

	switch {
	case oursEqTheirs:
		// Both sides agree (possibly both unchanged from base).
		data, err := ours.load()
		if err != nil {
			return ResultFile{}, false, err
		}
		return ResultFile{RelPath: rel, Bytes: data, Mode: ours.mode}, false, nil
	case oursEqBase && !theirsEqBase:
		data, err := theirs.load()
		if err != nil {
			return ResultFile{}, false, err
		}
		return ResultFile{RelPath: rel, Bytes: data, Mode: theirs.mode}, false, nil
	case theirsEqBase && !oursEqBase:
		data, err := ours.load()
		if err != nil {
			return ResultFile{}, false, err
		}
		return ResultFile{RelPath: rel, Bytes: data, Mode: ours.mode}, false, nil
	}

	// Both sides diverged from base. Attempt line-based merge if all
	// three files are textual; otherwise emit a whole-file conflict
	// envelope so the caller still sees something deterministic.
	baseBytes, err := base.load()
	if err != nil {
		return ResultFile{}, false, err
	}
	ourBytes, err := ours.load()
	if err != nil {
		return ResultFile{}, false, err
	}
	theirBytes, err := theirs.load()
	if err != nil {
		return ResultFile{}, false, err
	}
	if m.isBinary(baseBytes) || m.isBinary(ourBytes) || m.isBinary(theirBytes) {
		body := m.renderBinaryConflict(rel, ourBytes, baseBytes, theirBytes)
		return ResultFile{RelPath: rel, Bytes: body, Mode: ours.mode, Conflict: true}, true, nil
	}

	merged, conflicted := diff3Merge(splitLines(ourBytes), splitLines(baseBytes), splitLines(theirBytes), m.opts)
	return ResultFile{RelPath: rel, Bytes: merged, Mode: ours.mode, Conflict: conflicted}, conflicted, nil
}

func (m *merger) mergeBothAdded(rel string, ours, theirs *fileEntry) (ResultFile, bool, error) {
	same, err := sameContent(ours, theirs)
	if err != nil {
		return ResultFile{}, false, err
	}
	if same {
		data, err := ours.load()
		if err != nil {
			return ResultFile{}, false, err
		}
		return ResultFile{RelPath: rel, Bytes: data, Mode: ours.mode}, false, nil
	}
	ourBytes, err := ours.load()
	if err != nil {
		return ResultFile{}, false, err
	}
	theirBytes, err := theirs.load()
	if err != nil {
		return ResultFile{}, false, err
	}
	if m.isBinary(ourBytes) || m.isBinary(theirBytes) {
		body := m.renderBinaryConflict(rel, ourBytes, nil, theirBytes)
		return ResultFile{RelPath: rel, Bytes: body, Mode: ours.mode, Conflict: true}, true, nil
	}
	// Treat "no base" as an empty base: every line is a conflicting add.
	merged, conflicted := diff3Merge(splitLines(ourBytes), nil, splitLines(theirBytes), m.opts)
	return ResultFile{RelPath: rel, Bytes: merged, Mode: ours.mode, Conflict: conflicted}, conflicted, nil
}

// renderBinaryConflict produces a deterministic textual envelope for
// binary-mode conflicts. Callers should treat the output as opaque and
// surface the file path to the user.
func (m *merger) renderBinaryConflict(rel string, ours, base, theirs []byte) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s\n", m.opts.MarkerOurs)
	fmt.Fprintf(&buf, "binary file %s (%d bytes, sha256=%x)\n", rel, len(ours), sha256.Sum256(ours))
	fmt.Fprintf(&buf, "%s\n", m.opts.MarkerBase)
	if base == nil {
		fmt.Fprintf(&buf, "(absent in base)\n")
	} else {
		fmt.Fprintf(&buf, "binary file %s (%d bytes, sha256=%x)\n", rel, len(base), sha256.Sum256(base))
	}
	fmt.Fprintf(&buf, "%s\n", m.opts.MarkerTheirs)
	fmt.Fprintf(&buf, "binary file %s (%d bytes, sha256=%x)\n", rel, len(theirs), sha256.Sum256(theirs))
	fmt.Fprintf(&buf, "%s\n", m.opts.MarkerEnd)
	return buf.Bytes()
}

func (m *merger) renderDeleteModifyConflict(rel, deletedSide, modifiedSide string, base, modified []byte) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s\n", m.opts.MarkerOurs)
	fmt.Fprintf(&buf, "%s deleted %s\n", deletedSide, rel)
	fmt.Fprintf(&buf, "%s\n", m.opts.MarkerBase)
	if base == nil {
		fmt.Fprintf(&buf, "(absent in base)\n")
	} else {
		buf.Write(base)
		if len(base) == 0 || base[len(base)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	fmt.Fprintf(&buf, "%s\n", m.opts.MarkerTheirs)
	fmt.Fprintf(&buf, "%s modified %s:\n", modifiedSide, rel)
	buf.Write(modified)
	if len(modified) == 0 || modified[len(modified)-1] != '\n' {
		buf.WriteByte('\n')
	}
	fmt.Fprintf(&buf, "%s\n", m.opts.MarkerEnd)
	return buf.Bytes()
}

// isBinary returns true if the leading probe bytes contain a NUL byte.
// This matches the heuristic used by `git diff` and is good enough for
// the canonical files we expect (markdown, yaml, shell scripts).
func (m *merger) isBinary(data []byte) bool {
	probe := data
	if len(probe) > m.opts.BinaryProbeBytes {
		probe = probe[:m.opts.BinaryProbeBytes]
	}
	return bytes.IndexByte(probe, 0) != -1
}

// indexDir walks dir and returns a map of forward-slash relative paths
// to fileEntry handles. A non-existent dir yields an empty index — this
// is the documented behavior for missing-base merges.
func indexDir(dir string) (sideIndex, error) {
	idx := sideIndex{}
	if dir == "" {
		return idx, nil
	}
	root, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return idx, nil
		}
		return nil, fmt.Errorf("merge: stat %s: %w", dir, err)
	}
	if !root.IsDir() {
		return nil, fmt.Errorf("merge: %s is not a directory", dir)
	}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		// Skip symlinks for safety; the canonical store does not write
		// them inside skill dirs.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		idx[filepath.ToSlash(rel)] = &fileEntry{
			path:    path,
			relPath: filepath.ToSlash(rel),
			mode:    info.Mode().Perm(),
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("merge: walk %s: %w", dir, err)
	}
	return idx, nil
}

// unionKeys returns the sorted union of paths across the three indexes.
func unionKeys(a, b, c sideIndex) []string {
	seen := make(map[string]struct{}, len(a)+len(b)+len(c))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	for k := range c {
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sameContent returns true if both entries hash to the same SHA-256
// digest. A nil entry contributes no bytes (the digest of an empty
// stream); both-nil compares equal.
func sameContent(a, b *fileEntry) (bool, error) {
	ah, err := contentHash(a)
	if err != nil {
		return false, err
	}
	bh, err := contentHash(b)
	if err != nil {
		return false, err
	}
	return ah == bh, nil
}

func contentHash(e *fileEntry) ([32]byte, error) {
	var zero [32]byte
	if e == nil {
		return zero, nil
	}
	data, err := e.load()
	if err != nil {
		return zero, err
	}
	return sha256.Sum256(data), nil
}

// splitLines splits data on '\n', keeping the terminator. A trailing
// chunk without '\n' is returned as its own line so the diff3 algorithm
// can round-trip files that don't end in a newline.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	out := make([]string, 0, bytes.Count(data, []byte{'\n'})+1)
	start := 0
	for i, b := range data {
		if b == '\n' {
			out = append(out, string(data[start:i+1]))
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, string(data[start:]))
	}
	return out
}
