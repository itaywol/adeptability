// Package locks reads and writes .adeptability/adept.lock.json — a small
// ledger that records the upstream provenance of skills installed from
// external sources (today: skills.sh / GitHub).
//
// Locally-authored skills do NOT live in the lockfile. The hash-is-truth
// principle from v0.2 still holds for everything in project canonical that
// was not pulled from an external source. The lockfile exists only to
// pin "this skill came from THAT upstream at THIS sha, content was THIS
// hash" so we can:
//
//   - reproduce the install on another machine (skill install --from-lock)
//   - bump the upstream cleanly (skill update)
//   - warn the user when the on-disk skill drifted from what we recorded
//     at install time (hash-verify before sync)
//
// The file is JSON, schema-versioned, atomic-rename written.
package locks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SchemaVersion is the current lockfile schema version. Increment when
// the on-disk shape changes in a non-additive way.
const SchemaVersion = 1

// FileName lives alongside config.json under .adeptability/.
const FileName = "adept.lock.json"

// Source identifies WHERE a skill came from. Keeping this as an enum
// (string under the hood) lets us add npm, jsr, OCI, etc. later without
// schema breakage.
type Source string

const (
	SourceSkillsSh Source = "skills.sh"
	SourceGitHub   Source = "github"
)

// Entry is one row in the lockfile. Every external-installed project
// skill has exactly one Entry, keyed by skill id in the parent Lock.
type Entry struct {
	// Source is the registry the skill was pulled from.
	Source Source `json:"source"`
	// Slug is the upstream identifier as the user passes it on the
	// command line. For skills.sh: "<owner>/<repo>/<skill>". For raw
	// GitHub: "<owner>/<repo>/<skill>" or "<owner>/<repo>#<ref>/<skill>".
	Slug string `json:"slug"`
	// Repo is the canonical https URL of the upstream git repo.
	Repo string `json:"repo"`
	// Ref is the user-visible reference (branch or tag) that was
	// requested. Distinct from SHA because branches move.
	Ref string `json:"ref,omitempty"`
	// SHA is the immutable commit hash of the install. Re-installs
	// reproduce content from this SHA.
	SHA string `json:"sha"`
	// SkillPath is the path within the repo (relative to the repo root)
	// where SKILL.md lives. Encodes layout convention discovery.
	SkillPath string `json:"skillPath"`
	// ContentHash is the sha256 of the canonical skill directory as
	// installed. `sync` re-hashes before rendering and warns on drift.
	ContentHash string `json:"contentHash"`
	// InstalledAt records when the user installed the skill. Aids
	// debugging stale installs and supports `skill update --older-than`.
	InstalledAt time.Time `json:"installedAt"`
}

// Lock is the top-level on-disk shape. External is keyed by SKILL ID,
// not slug, so each project canonical id maps to at most one upstream
// provenance.
type Lock struct {
	Schema   int              `json:"schema"`
	External map[string]Entry `json:"external,omitempty"`
}

// New returns an empty Lock at the current schema version.
func New() *Lock {
	return &Lock{Schema: SchemaVersion}
}

// Load reads the lockfile at path. Missing file returns an empty Lock
// with no error so callers can treat first-run as "nothing locked yet".
// Schema mismatches return a typed error so the CLI can surface a clean
// upgrade hint instead of a generic JSON failure.
func Load(path string) (*Lock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return New(), nil
		}
		return nil, fmt.Errorf("read lockfile %s: %w", path, err)
	}
	var probe struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse lockfile %s: %w", path, err)
	}
	if probe.Schema != SchemaVersion {
		return nil, fmt.Errorf("lockfile %s: schema %d unsupported (want %d)", path, probe.Schema, SchemaVersion)
	}
	out := New()
	if err := json.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("parse lockfile %s: %w", path, err)
	}
	if out.External == nil {
		out.External = map[string]Entry{}
	}
	return out, nil
}

// Save writes lock to path atomically (temp + rename). It creates the
// parent directory if missing and uses a unique per-writer temp file
// (os.CreateTemp) so two concurrent writers never share a temp path and
// clobber each other's partial write. Note: atomic rename guarantees the
// final file is never torn, but it does NOT serialize a read-modify-write
// across processes — callers needing lost-update safety must take an
// advisory file lock around Load/Set/Save.
func Save(path string, lock *Lock) error {
	if lock == nil {
		return fmt.Errorf("save lockfile %s: nil lock", path)
	}
	if lock.Schema == 0 {
		lock.Schema = SchemaVersion
	}
	// Marshal with stable key order: schema first, then external map
	// (which json.Marshal already sorts by key alphabetically).
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("ensure lockfile dir %s: %w", dir, err)
	}
	tmpf, err := os.CreateTemp(dir, "adept.lock-*.json")
	if err != nil {
		return fmt.Errorf("create tmp lockfile: %w", err)
	}
	tmp := tmpf.Name()
	// Best-effort cleanup of the temp file on any failure before rename.
	defer func() { _ = os.Remove(tmp) }()
	if _, err := tmpf.Write(data); err != nil {
		_ = tmpf.Close()
		return fmt.Errorf("write tmp lockfile: %w", err)
	}
	if err := tmpf.Close(); err != nil {
		return fmt.Errorf("close tmp lockfile: %w", err)
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return fmt.Errorf("chmod tmp lockfile: %w", err)
	}
	if err := renameWithRetry(tmp, path); err != nil {
		return fmt.Errorf("rename lockfile: %w", err)
	}
	return nil
}

// renameWithRetry replaces newpath with oldpath, retrying briefly. On Windows
// an atomic rename over an existing file can transiently fail with "Access is
// denied" when a concurrent writer is replacing the same target; a bounded
// retry lets the writers serialize. On POSIX the first attempt succeeds.
func renameWithRetry(oldpath, newpath string) error {
	var err error
	for i := 0; i < 20; i++ {
		if err = os.Rename(oldpath, newpath); err == nil {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return err
}

// Set upserts an entry by skill id and returns the same Lock for chaining.
func (l *Lock) Set(id string, e Entry) *Lock {
	if l.External == nil {
		l.External = map[string]Entry{}
	}
	l.External[id] = e
	return l
}

// Get returns the entry for id and whether it was found.
func (l *Lock) Get(id string) (Entry, bool) {
	e, ok := l.External[id]
	return e, ok
}

// Delete removes id from the lock. No-op when absent.
func (l *Lock) Delete(id string) {
	delete(l.External, id)
}

// IDs returns the locked skill ids in deterministic order.
func (l *Lock) IDs() []string {
	out := make([]string, 0, len(l.External))
	for id := range l.External {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
