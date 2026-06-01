// Package lockfile reads and writes adeptability.lock.json (schema 2).
//
// Writes are atomic: bytes are flushed to a temp file in the same directory,
// then renamed into place. Read rejects unknown or stale schemas with
// adept.ErrLockSchemaMismatch — callers can then invoke MigrateFromSkillbook.
package lockfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/itaywol/adeptability/pkg/adept"
)

// WriteFunc is the injected file-write dependency. Implementations MUST be
// atomic (write to temp, fsync, rename) so partial writes can never produce a
// half-written lockfile.
type WriteFunc func(path string, data []byte, mode os.FileMode) error

// Store reads, mutates, and writes lockfiles.
type Store interface {
	Read(path string) (*adept.LockFile, error)
	Write(path string, lf *adept.LockFile) error
	Empty() *adept.LockFile
	SetEntry(lf *adept.LockFile, id string, e adept.LockEntry) *adept.LockFile
	SetHarnessMode(lf *adept.LockFile, h string, m adept.HarnessMode) *adept.LockFile
	GetHarnessMode(lf *adept.LockFile, h string) adept.HarnessMode
}

type store struct {
	write WriteFunc
}

// NewStore returns a Store that delegates writes to write. Pass nil to use the
// built-in atomic writer.
func NewStore(write WriteFunc) Store {
	if write == nil {
		write = defaultAtomicWrite
	}
	return &store{write: write}
}

// Empty returns a fresh schema=2 lockfile with an initialized skills map.
func (s *store) Empty() *adept.LockFile {
	return &adept.LockFile{
		Schema: adept.LockSchemaVersion,
		Skills: map[string]adept.LockEntry{},
	}
}

// Read parses path. Returns adept.ErrSkillNotFound semantics by wrapping
// os.ErrNotExist for missing files; the caller can decide whether to treat
// that as "no lockfile yet".
func (s *store) Read(path string) (*adept.LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lockfile: read %s: %w", path, err)
	}
	var lf adept.LockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("lockfile: parse %s: %w", path, err)
	}
	if lf.Schema != adept.LockSchemaVersion {
		return nil, fmt.Errorf("lockfile: %s: %w: got schema=%d, want %d",
			path, adept.ErrLockSchemaMismatch, lf.Schema, adept.LockSchemaVersion)
	}
	if lf.Skills == nil {
		lf.Skills = map[string]adept.LockEntry{}
	}
	return &lf, nil
}

// Write atomically writes lf to path. Marshals with stable key order
// (encoding/json sorts map keys alphabetically; struct fields keep declared
// order).
func (s *store) Write(path string, lf *adept.LockFile) error {
	if lf == nil {
		return fmt.Errorf("lockfile: cannot write nil lockfile")
	}
	if lf.Schema == 0 {
		lf.Schema = adept.LockSchemaVersion
	}
	if lf.Schema != adept.LockSchemaVersion {
		return fmt.Errorf("lockfile: write %s: %w: refusing to write schema=%d",
			path, adept.ErrLockSchemaMismatch, lf.Schema)
	}
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("lockfile: marshal: %w", err)
	}
	data = append(data, '\n')
	return s.write(path, data, 0o644)
}

// SetEntry returns lf with the skill entry assigned. Allocates the skills map
// lazily.
func (s *store) SetEntry(lf *adept.LockFile, id string, e adept.LockEntry) *adept.LockFile {
	if lf == nil {
		lf = s.Empty()
	}
	if lf.Skills == nil {
		lf.Skills = map[string]adept.LockEntry{}
	}
	lf.Skills[id] = e
	return lf
}

// SetHarnessMode assigns mode for harness h and ensures it is present in the
// harnesses list.
func (s *store) SetHarnessMode(lf *adept.LockFile, h string, m adept.HarnessMode) *adept.LockFile {
	if lf == nil {
		lf = s.Empty()
	}
	if lf.HarnessModes == nil {
		lf.HarnessModes = map[string]adept.HarnessMode{}
	}
	lf.HarnessModes[h] = m
	found := false
	for _, existing := range lf.Harnesses {
		if existing == h {
			found = true
			break
		}
	}
	if !found {
		lf.Harnesses = append(lf.Harnesses, h)
	}
	return lf
}

// GetHarnessMode returns the configured mode for h, defaulting to ModeSymlink
// when no override is recorded.
func (s *store) GetHarnessMode(lf *adept.LockFile, h string) adept.HarnessMode {
	if lf == nil || lf.HarnessModes == nil {
		return adept.ModeSymlink
	}
	if m, ok := lf.HarnessModes[h]; ok && m != "" {
		return m
	}
	return adept.ModeSymlink
}

// defaultAtomicWrite writes data to a temp file in the same directory then
// renames it into place. fsync is best-effort; failures fall through to error.
func defaultAtomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := dirOf(path)
	tmp, err := os.CreateTemp(dir, ".lock-*.tmp")
	if err != nil {
		return fmt.Errorf("lockfile: temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("lockfile: write tmp %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("lockfile: sync tmp %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("lockfile: close tmp %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil && !errors.Is(err, fs.ErrPermission) {
		cleanup()
		return fmt.Errorf("lockfile: chmod tmp %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("lockfile: rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
