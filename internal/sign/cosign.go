package sign

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Cosign defaults match the goreleaser keyless signing configuration. They
// can be overridden via ADEPT_COSIGN_IDENTITY_REGEXP and
// ADEPT_COSIGN_OIDC_ISSUER env vars (see Detect).
const (
	DefaultCosignIdentityRegexp = `https://github\.com/itaywol/adeptability/`
	DefaultCosignOIDCIssuer     = "https://token.actions.githubusercontent.com"

	// EnvCosignIdentity overrides the certificate-identity regexp.
	EnvCosignIdentity = "ADEPT_COSIGN_IDENTITY_REGEXP"
	// EnvCosignIssuer overrides the OIDC issuer.
	EnvCosignIssuer = "ADEPT_COSIGN_OIDC_ISSUER"
)

// CommandRunner abstracts exec.CommandContext for testability. Implementations
// must produce a process whose Output/Run/CombinedOutput methods behave like
// *exec.Cmd. The runner is created lazily so each Verify call gets a fresh
// process.
type CommandRunner interface {
	// Run executes name with args and returns the combined stdout+stderr
	// output. Non-nil err signals a non-zero exit (the bytes still carry
	// whatever cosign printed).
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner is the production CommandRunner backed by os/exec.
type ExecRunner struct{}

// Run shells out to name with args, capturing combined output.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// ErrCosignBinaryMissing is returned by NewCosignVerifier when the binary
// path is empty or does not exist on the filesystem.
var ErrCosignBinaryMissing = errors.New("cosign binary missing")

// ErrCosignVerifyFailed wraps a non-zero exit from cosign verify-blob.
var ErrCosignVerifyFailed = errors.New("cosign verify failed")

// CosignVerifier implements Verifier by shelling out to the cosign binary.
//
// We deliberately avoid the sigstore SDK: it transitively pulls grpc, etcd,
// and a handful of large helper modules — ~30 MB of binary growth. Subprocess
// invocation is the right trade-off for a CLI shipped as a static binary.
type CosignVerifier struct {
	binary   string
	identity string
	issuer   string
	runner   CommandRunner
}

// NewCosignVerifier constructs a CosignVerifier. The binary path must be a
// resolved absolute path (use exec.LookPath upstream). identity/issuer
// default to the goreleaser-keyless constants when empty. runner defaults to
// ExecRunner; tests inject a stub.
func NewCosignVerifier(binary, identity, issuer string, runner CommandRunner) (*CosignVerifier, error) {
	if binary == "" {
		return nil, fmt.Errorf("new cosign verifier: %w", ErrCosignBinaryMissing)
	}
	if _, err := os.Stat(binary); err != nil {
		return nil, fmt.Errorf("new cosign verifier: stat %q: %w", binary, ErrCosignBinaryMissing)
	}
	if identity == "" {
		identity = DefaultCosignIdentityRegexp
	}
	if issuer == "" {
		issuer = DefaultCosignOIDCIssuer
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	return &CosignVerifier{
		binary:   binary,
		identity: identity,
		issuer:   issuer,
		runner:   runner,
	}, nil
}

// Verify writes blob/sig/cert to a temp directory and shells out to
// `cosign verify-blob`. Non-zero exit codes are wrapped with the captured
// output so callers can surface cosign's diagnostic message.
func (v *CosignVerifier) Verify(ctx context.Context, blob, sig, cert []byte) error {
	if v == nil {
		return errors.New("cosign verifier: nil receiver")
	}
	if len(blob) == 0 {
		return errors.New("cosign verifier: empty blob")
	}
	if len(sig) == 0 {
		return errors.New("cosign verifier: empty signature")
	}
	if len(cert) == 0 {
		return errors.New("cosign verifier: empty certificate")
	}

	dir, err := os.MkdirTemp("", "adept-cosign-*")
	if err != nil {
		return fmt.Errorf("cosign verifier: tempdir: %w", err)
	}
	defer os.RemoveAll(dir)

	blobPath := filepath.Join(dir, "blob")
	sigPath := filepath.Join(dir, "sig")
	certPath := filepath.Join(dir, "cert")

	for _, w := range []struct {
		path string
		data []byte
	}{
		{blobPath, blob},
		{sigPath, sig},
		{certPath, cert},
	} {
		if err := os.WriteFile(w.path, w.data, 0o600); err != nil {
			return fmt.Errorf("cosign verifier: write %s: %w", filepath.Base(w.path), err)
		}
	}

	args := []string{
		"verify-blob",
		"--certificate", certPath,
		"--signature", sigPath,
		"--certificate-identity-regexp", v.identity,
		"--certificate-oidc-issuer", v.issuer,
		blobPath,
	}
	out, err := v.runner.Run(ctx, v.binary, args...)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrCosignVerifyFailed, trimOutput(out), err)
	}
	return nil
}

// trimOutput collapses a cosign output blob into a single-line summary
// suitable for embedding in an error. The full output may include multiple
// lines, but errors should remain readable.
func trimOutput(b []byte) string {
	s := bytes.TrimSpace(b)
	if len(s) == 0 {
		return "(no output)"
	}
	// Replace newlines with " | " so the error fits on one line.
	out := bytes.ReplaceAll(s, []byte("\n"), []byte(" | "))
	const maxLen = 512
	if len(out) > maxLen {
		out = append(out[:maxLen], []byte("…")...)
	}
	return string(out)
}

// io.Discard is referenced indirectly via the runner; keep the import alive
// in case future refactors reintroduce streaming.
var _ io.Writer = io.Discard
