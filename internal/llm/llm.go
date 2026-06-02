// Package llm is the provider-agnostic LLM client used by the optional
// safety-scan intent pass.
//
// The contract is intentionally minimal: a Provider knows its own name,
// can self-test whether it's reachable, and can Evaluate one prompt at
// a time returning structured text or JSON.
//
// API keys are NEVER passed via Config — providers read them from
// environment variables (`ANTHROPIC_API_KEY`, etc.) at call time so
// secrets stay out of the project tree and the on-disk config file. The
// only thing recorded is which provider + model the project picked.
package llm

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Request is a single LLM call. Implementations pick the right wire
// shape per provider; the user-facing prompt content stays the same.
type Request struct {
	// System is the canonical "system prompt" — provider-specific
	// wrapping (top-level "system" field for Anthropic vs the first
	// message for OpenAI-style chat) is handled inside the impl.
	System string
	// User is the per-call user prompt.
	User string
	// Model overrides the provider's default model when non-empty.
	Model string
	// MaxTokens caps the response length. 0 means provider default.
	MaxTokens int
	// JSONMode true asks the provider to return strict JSON. The
	// caller is responsible for matching the prompt to the expected
	// schema; this flag just hints "no prose".
	JSONMode bool
}

// Response carries the model's reply. Text is the raw string content;
// JSONMode requests should leave Text as the JSON-encoded body too so
// callers don't need a separate field for the deserialized shape.
type Response struct {
	Text   string
	Usage  Usage
	Model  string
	Reason string // optional stop reason from the provider
}

// Usage is a coarse-grained token accounting helper used for logging
// and surfacing approximate cost in `adept skill check`. Best-effort —
// providers that don't expose token counts leave the fields at zero.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Provider is the contract every concrete LLM client implements.
type Provider interface {
	// Name is the lowercase identifier the user types into `adept config
	// llm set <name>`.
	Name() string
	// DefaultModel is the model used when LLMConfig.Model is empty.
	DefaultModel() string
	// Available is a lightweight reachability probe — no API call, just
	// "does this provider have what it needs to run?" (env var, local
	// endpoint, etc.). Used by `adept config llm test` and by the
	// install-time scan gate to decide whether to attempt a call.
	Available(ctx context.Context) error
	// Evaluate sends one Request and returns the Response.
	Evaluate(ctx context.Context, req Request) (Response, error)
}

// Registry resolves provider names to concrete Providers. Concrete
// providers register themselves at init() time; tests can build their
// own Registry.
type Registry interface {
	Get(name string) (Provider, error)
	List() []string
}

// NewRegistry returns a Registry populated with the given providers.
// Order in the input slice is preserved by List().
func NewRegistry(providers ...Provider) Registry {
	r := &registry{providers: map[string]Provider{}}
	for _, p := range providers {
		r.providers[p.Name()] = p
		r.order = append(r.order, p.Name())
	}
	return r
}

type registry struct {
	providers map[string]Provider
	order     []string
}

// ErrProviderUnknown is returned when Get can't find a registered
// provider — the CLI lifts it to a user-friendly message.
var ErrProviderUnknown = errors.New("llm provider unknown")

func (r *registry) Get(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q (known: %v)", ErrProviderUnknown, name, r.List())
	}
	return p, nil
}

func (r *registry) List() []string {
	out := append([]string{}, r.order...)
	sort.Strings(out)
	return out
}
