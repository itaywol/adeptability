// Package harness implements the registry and orchestration logic for
// harness adapters. Adapters are registered at startup (built-ins +
// YAML-loaded synthetics) and the Orchestrator uses them to translate a
// project's canonical skills into harness-specific files on disk.
package harness

import (
	"fmt"
	"sort"
	"sync"

	"github.com/itaywol/adeptability/pkg/adept"
)

// Registry keeps track of every adapter known to the running CLI.
// Implementations must be safe for concurrent reads after registration is
// complete; Register itself does NOT need to be concurrency-safe relative to
// reads because the CLI registers all adapters before launching workers.
type Registry interface {
	Register(a adept.HarnessAdapter) error
	Get(id string) (adept.HarnessAdapter, error)
	List() []adept.HarnessAdapter
}

// NewRegistry returns an empty in-memory registry.
func NewRegistry() Registry {
	return &registry{adapters: map[string]adept.HarnessAdapter{}}
}

type registry struct {
	mu       sync.RWMutex
	adapters map[string]adept.HarnessAdapter
}

func (r *registry) Register(a adept.HarnessAdapter) error {
	if a == nil {
		return fmt.Errorf("registry: %w: nil adapter", adept.ErrAdapterInvalid)
	}
	spec := a.Spec()
	if spec.ID == "" {
		return fmt.Errorf("registry: %w: empty adapter id", adept.ErrAdapterInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.adapters[spec.ID]; exists {
		return fmt.Errorf("registry: %w: %q already registered", adept.ErrAdapterInvalid, spec.ID)
	}
	r.adapters[spec.ID] = a
	return nil
}

func (r *registry) Get(id string) (adept.HarnessAdapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[id]
	if !ok {
		return nil, fmt.Errorf("registry get %q: %w", id, adept.ErrHarnessUnknown)
	}
	return a, nil
}

func (r *registry) List() []adept.HarnessAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]adept.HarnessAdapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec().ID < out[j].Spec().ID })
	return out
}
