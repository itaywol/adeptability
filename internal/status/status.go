// Package status is a pure state machine that resolves the sync state of a
// single skill from three observations:
//
//  1. The hash of the skill content currently on disk in the project.
//  2. The lockfile entry recorded by the project (if any).
//  3. The lockfile entry recorded by the central library (if any).
//
// No I/O happens here.
package status

import "github.com/itaywol/adeptability/pkg/adept"

// Input is everything the resolver needs to make a decision.
type Input struct {
	// ProjectHash is the hash of the on-disk skill in the project.
	// Empty string means the skill is not on disk in the project.
	ProjectHash string
	// ProjectEntry is the project lockfile entry. nil = no project lock.
	ProjectEntry *adept.LockEntry
	// LibraryEntry is the library lockfile entry. nil = no library entry.
	LibraryEntry *adept.LockEntry
}

// Resolver turns an Input into a Status.
type Resolver interface {
	Resolve(in Input) adept.Status
}

type resolver struct{}

// NewResolver returns a stateless Resolver.
func NewResolver() Resolver {
	return &resolver{}
}

// Resolve applies the documented transition table.
//
// Decision order:
//  1. Skill not on disk + no library entry -> LocalOnly (effectively absent).
//  2. Skill not on disk + library entry present -> LibraryOnly.
//  3. No project entry OR no library entry (and skill is on disk) -> LocalOnly.
//  4. project changed + library advanced -> Diverged.
//  5. project changed -> Ahead.
//  6. library advanced -> Behind.
//  7. otherwise -> Synced.
func (r *resolver) Resolve(in Input) adept.Status {
	onDisk := in.ProjectHash != ""

	if !onDisk {
		if in.LibraryEntry != nil {
			return adept.StatusLibraryOnly
		}
		return adept.StatusLocalOnly
	}

	if in.ProjectEntry == nil || in.LibraryEntry == nil {
		return adept.StatusLocalOnly
	}

	projectChanged := in.ProjectHash != in.ProjectEntry.Hash
	libraryAdvanced := in.LibraryEntry.Version != in.ProjectEntry.Version ||
		in.LibraryEntry.Hash != in.ProjectEntry.Hash

	switch {
	case projectChanged && libraryAdvanced:
		return adept.StatusDiverged
	case projectChanged:
		return adept.StatusAhead
	case libraryAdvanced:
		return adept.StatusBehind
	default:
		return adept.StatusSynced
	}
}
