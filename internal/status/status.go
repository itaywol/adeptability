// Package status is a pure state machine that resolves the sync state of a
// single skill from three content hashes:
//
//  1. The hash of the project canonical (<root>/.adeptability/skills/<id>/).
//  2. The hash of the last-synced base snapshot (<root>/.adeptability/base/<id>/).
//  3. The hash of the library canonical ($ADEPT_LIBRARY/skills/<id>/).
//
// No I/O happens here — callers hash directories themselves and feed the
// resolver three strings. An empty string means "absent from that side".
package status

import "github.com/itaywol/adeptability/pkg/adept"

// Input is everything the resolver needs to make a decision. All three fields
// are content-hash strings (e.g. "sha256:<hex>"). Empty string means the
// corresponding side is absent on disk.
type Input struct {
	// ProjectHash is the hash of the on-disk skill in the project canonical.
	// Empty string = skill not present in the project.
	ProjectHash string
	// BaseHash is the hash of the last-synced base snapshot for this skill.
	// Empty string = no base snapshot recorded (skill has never been synced).
	BaseHash string
	// LibraryHash is the hash of the skill in the central library.
	// Empty string = skill not present in the library.
	LibraryHash string
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
//  1. Project absent + library absent -> LocalOnly (effectively absent).
//  2. Project absent + library present -> LibraryOnly.
//  3. Project present + library absent -> LocalOnly.
//  4. Project present + library present + no base -> LocalOnly
//     (installed but never synced; treat as freshly local).
//  5. Project changed (vs base) + library changed (vs base) -> Diverged.
//  6. Project changed only -> Ahead.
//  7. Library changed only -> Behind.
//  8. Neither side changed vs base -> Synced.
func (resolver) Resolve(in Input) adept.Status {
	switch {
	case in.ProjectHash == "" && in.LibraryHash == "":
		return adept.StatusLocalOnly
	case in.ProjectHash == "" && in.LibraryHash != "":
		return adept.StatusLibraryOnly
	case in.ProjectHash != "" && in.LibraryHash == "":
		return adept.StatusLocalOnly
	case in.BaseHash == "":
		// Installed but no base — treat as freshly local.
		return adept.StatusLocalOnly
	}
	projectChanged := in.ProjectHash != in.BaseHash
	libraryChanged := in.LibraryHash != in.BaseHash
	switch {
	case projectChanged && libraryChanged:
		return adept.StatusDiverged
	case projectChanged:
		return adept.StatusAhead
	case libraryChanged:
		return adept.StatusBehind
	}
	return adept.StatusSynced
}
