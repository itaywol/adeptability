// Package sign defines the signature verification contract used to validate
// signed skill bundles. Two backends ship: a Noop fallback and a CosignKeyless
// verifier that shells out to the cosign binary.
package sign

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Verifier checks that sig over blob is valid given cert.
type Verifier interface {
	Verify(ctx context.Context, blob, sig, cert []byte) error
}

// Signer produces a detached signature and certificate for blob.
type Signer interface {
	Sign(ctx context.Context, blob []byte) (sig, cert []byte, err error)
}

// Backend identifies which verifier implementation is wired.
type Backend string

const (
	// BackendNoop is the no-op verifier; every call succeeds.
	BackendNoop Backend = "noop"
	// BackendCosignKeyless shells out to `cosign verify-blob` using OIDC
	// keyless attestation.
	BackendCosignKeyless Backend = "cosign"
	// BackendDisabled disables verification entirely. It returns the Noop
	// pair but is reported distinctly so callers can flag the policy.
	BackendDisabled Backend = "disabled"
)

// EnvBackend is the env var that pins the signing backend. Recognized values
// are "disabled", "noop", "cosign", and the empty string (auto-detect).
const EnvBackend = "ADEPT_SIGN"

// Noop is a Verifier+Signer that always succeeds with empty outputs. It is
// used when the project explicitly opts out or no cosign binary is on PATH.
type Noop struct{}

// Verify always returns nil.
func (Noop) Verify(_ context.Context, _, _, _ []byte) error { return nil }

// Sign always returns empty sig and cert.
func (Noop) Sign(_ context.Context, _ []byte) (sig, cert []byte, err error) {
	return nil, nil, nil
}

// NewNoop returns a Verifier and Signer pair backed by Noop.
func NewNoop() (Verifier, Signer) { return Noop{}, Noop{} }

// LookPathFunc is the contract Detect uses to discover the cosign binary.
// Tests inject a stub; production calls exec.LookPath.
type LookPathFunc func(name string) (string, error)

// DetectOptions captures the inputs to Detect. Zero values are safe; the
// caller typically only sets EnvOverride explicitly when honoring an
// explicit CLI flag, otherwise leaves it empty for auto-detect via env.
type DetectOptions struct {
	// EnvOverride pins the backend regardless of env var or PATH. Empty =
	// fall back to os.Getenv(EnvBackend).
	EnvOverride string
	// LookPath defaults to exec.LookPath.
	LookPath LookPathFunc
	// Identity/Issuer override the cosign defaults when CosignKeyless is
	// selected. Empty values fall back to env vars, then the goreleaser
	// constants.
	Identity string
	Issuer   string
}

// DetectResult is the outcome of Detect: the chosen backend, the wired
// Verifier+Signer pair, and an optional notice the CLI should log at info
// level (e.g. when falling back to Noop because cosign isn't installed).
type DetectResult struct {
	Backend  Backend
	Verifier Verifier
	Signer   Signer
	Notice   string
}

// ErrUnknownBackend is returned by Detect when the env override is set to a
// value that is not one of "disabled", "noop", "cosign", or empty.
var ErrUnknownBackend = errors.New("unknown signing backend")

// Detect chooses the best signing backend given the options and environment:
//
//	disabled         -> Noop pair, BackendDisabled (policy: trust nothing)
//	noop             -> Noop pair, BackendNoop      (policy: legacy/dev only)
//	cosign           -> CosignVerifier (fails fast if binary absent)
//	"" (auto-detect) -> cosign if available on PATH, else Noop with a notice
//
// Identity/issuer fall back to env vars (ADEPT_COSIGN_IDENTITY_REGEXP /
// ADEPT_COSIGN_OIDC_ISSUER) and finally to the goreleaser constants.
func Detect(opts DetectOptions) (*DetectResult, error) {
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	choice := strings.ToLower(strings.TrimSpace(opts.EnvOverride))
	if choice == "" {
		choice = strings.ToLower(strings.TrimSpace(os.Getenv(EnvBackend)))
	}

	identity := opts.Identity
	if identity == "" {
		identity = os.Getenv(EnvCosignIdentity)
	}
	issuer := opts.Issuer
	if issuer == "" {
		issuer = os.Getenv(EnvCosignIssuer)
	}

	switch choice {
	case "disabled":
		v, s := NewNoop()
		return &DetectResult{Backend: BackendDisabled, Verifier: v, Signer: s}, nil
	case "noop":
		v, s := NewNoop()
		return &DetectResult{Backend: BackendNoop, Verifier: v, Signer: s}, nil
	case "cosign":
		binary, err := lookPath("cosign")
		if err != nil {
			return nil, fmt.Errorf("sign detect: ADEPT_SIGN=cosign requested but binary not found on PATH; install cosign (https://docs.sigstore.dev/system_config/installation/): %w", err)
		}
		v, err := NewCosignVerifier(binary, identity, issuer, ExecRunner{})
		if err != nil {
			return nil, fmt.Errorf("sign detect: %w", err)
		}
		_, sgn := NewNoop()
		return &DetectResult{Backend: BackendCosignKeyless, Verifier: v, Signer: sgn}, nil
	case "":
		// Auto-detect.
		binary, err := lookPath("cosign")
		if err == nil {
			v, verr := NewCosignVerifier(binary, identity, issuer, ExecRunner{})
			if verr == nil {
				_, sgn := NewNoop()
				return &DetectResult{Backend: BackendCosignKeyless, Verifier: v, Signer: sgn}, nil
			}
		}
		v, s := NewNoop()
		return &DetectResult{
			Backend:  BackendNoop,
			Verifier: v,
			Signer:   s,
			Notice:   "cosign binary not on PATH; signature verification is a no-op (set ADEPT_SIGN=cosign once installed, or ADEPT_SIGN=disabled to silence)",
		}, nil
	default:
		return nil, fmt.Errorf("%w: %q (want one of: disabled, noop, cosign, or empty)", ErrUnknownBackend, opts.EnvOverride)
	}
}
