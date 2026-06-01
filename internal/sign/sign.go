// Package sign defines the signature verification contract used to validate
// signed skill bundles. v0.1 ships only a Noop implementation; the real
// cosign-backed implementation is deferred to v0.2.
package sign

import "context"

// Verifier checks that sig over blob is valid given cert.
type Verifier interface {
	Verify(ctx context.Context, blob, sig, cert []byte) error
}

// Signer produces a detached signature and certificate for blob.
type Signer interface {
	Sign(ctx context.Context, blob []byte) (sig, cert []byte, err error)
}

// Noop is a Verifier+Signer that always succeeds with empty outputs. It is
// the only implementation shipped in v0.1 so the rest of the system can be
// wired without a hard dependency on cosign.
type Noop struct{}

// Verify always returns nil.
func (Noop) Verify(_ context.Context, _, _, _ []byte) error { return nil }

// Sign always returns empty sig and cert.
func (Noop) Sign(_ context.Context, _ []byte) (sig, cert []byte, err error) {
	return nil, nil, nil
}

// NewNoop returns a Verifier and Signer pair backed by Noop. Callers should
// treat the returned types as v0.1-only placeholders; v0.2 will introduce a
// real cosign-based verifier behind the same interfaces.
func NewNoop() (Verifier, Signer) { return Noop{}, Noop{} }
