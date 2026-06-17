// Package exchange implements the team "expertise billboard": a minimal
// self-hosted HTTP server plus the storage behind it. A developer posts a
// request for a teammate's expertise; teammates (or their agents) stack
// responses on it; the author reviews and closes it.
//
// The server is passive — it never runs an agent and holds no LLM keys.
// Agents participate by calling the adept CLI, which talks to this server.
package exchange

import (
	"fmt"
	"sort"

	"github.com/itaywol/adeptability/pkg/adept"
)

// ListFilter narrows ListItems. Empty fields match everything.
type ListFilter struct {
	// Mine restricts to items authored by or assigned to Handle.
	Mine bool
	// Handle is the caller's identity, used when Mine is set.
	Handle string
	// Status, when set, restricts to that item status.
	Status string
}

// Store is the persistence contract behind the billboard. Implementations
// must be safe for concurrent use. All identity/authorization policy lives
// in the HTTP server; the Store only enforces structural invariants.
type Store interface {
	// BootstrapHash returns the stored sha256 of the bootstrap token, or ""
	// if none has been set yet.
	BootstrapHash() (string, error)
	// SetBootstrapHash records (or rotates) the bootstrap token hash.
	SetBootstrapHash(hash string) error

	// CreateUser registers a new participant. Returns ErrExchangeHandleTaken
	// if the handle already exists.
	CreateUser(u adept.ExchangeUser) error
	// UserByTokenHash resolves an authenticated user from a token hash.
	UserByTokenHash(hash string) (adept.ExchangeUser, bool, error)
	// RotateUserToken replaces the token hash for the user that currently
	// holds oldHash, returning the updated user.
	RotateUserToken(oldHash, newHash string) (adept.ExchangeUser, bool, error)

	// CreateItem stores a new request and returns it with its assigned ID,
	// status, and timestamps populated.
	CreateItem(item adept.ExchangeItem) (adept.ExchangeItem, error)
	// GetItem returns the item by id, or ErrExchangeItemNotFound.
	GetItem(id int) (adept.ExchangeItem, error)
	// ListItems returns items matching the filter, newest first.
	ListItems(f ListFilter) ([]adept.ExchangeItem, error)
	// AddComment appends a response. If the item is attention-required it
	// auto-flips to in-progress. Returns the updated item.
	AddComment(id int, c adept.ExchangeComment) (adept.ExchangeItem, error)
	// SetStatus sets the item status (no authorization check). Returns the
	// updated item.
	SetStatus(id int, status string) (adept.ExchangeItem, error)
}

// Driver opens a Store rooted at dataDir. For the in-memory driver dataDir
// is ignored.
type Driver func(dataDir string) (Store, error)

// DriverRegistry resolves driver names to Stores. Mirrors llm.Registry.
type DriverRegistry interface {
	Register(name string, d Driver) error
	Open(name, dataDir string) (Store, error)
	List() []string
}

type driverRegistry struct {
	drivers map[string]Driver
}

// NewDriverRegistry returns a registry pre-populated with the built-in
// "memory" and "fs" drivers.
func NewDriverRegistry() DriverRegistry {
	r := &driverRegistry{drivers: map[string]Driver{}}
	_ = r.Register("memory", func(string) (Store, error) { return newMemStore(nil), nil })
	_ = r.Register("fs", openFSStore)
	return r
}

func (r *driverRegistry) Register(name string, d Driver) error {
	if name == "" || d == nil {
		return fmt.Errorf("exchange: register driver: empty name or nil driver")
	}
	r.drivers[name] = d
	return nil
}

func (r *driverRegistry) Open(name, dataDir string) (Store, error) {
	d, ok := r.drivers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q (known: %v)", adept.ErrExchangeDriverUnknown, name, r.List())
	}
	return d(dataDir)
}

func (r *driverRegistry) List() []string {
	out := make([]string, 0, len(r.drivers))
	for n := range r.drivers {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
