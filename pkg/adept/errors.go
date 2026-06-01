package adept

import "errors"

// Sentinel errors surfaced through the public API. Wrap with fmt.Errorf("...: %w", err).
var (
	ErrSkillNotFound      = errors.New("skill not found")
	ErrSkillInvalid       = errors.New("skill invalid")
	ErrLockSchemaMismatch = errors.New("lockfile schema mismatch")
	ErrBudgetOverflow     = errors.New("aggregator budget overflow")
	ErrAdapterInvalid     = errors.New("adapter invalid")
	ErrHarnessUnknown     = errors.New("harness unknown")
	ErrSymlinkUnsupported = errors.New("symlink unsupported on this filesystem")
	ErrMergeConflict      = errors.New("merge conflict")
	ErrMergeBaseMissing   = errors.New("merge base snapshot missing")
)
