package sign

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordingRunner is a CommandRunner that captures invocations and returns a
// canned response. Safe for concurrent use so race-tests stay clean.
type recordingRunner struct {
	mu      sync.Mutex
	calls   []recordedCall
	respOut []byte
	respErr error
}

type recordedCall struct {
	name string
	args []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]string, len(args))
	copy(cp, args)
	r.calls = append(r.calls, recordedCall{name: name, args: cp})
	return r.respOut, r.respErr
}

func (r *recordingRunner) lastCall(t *testing.T) recordedCall {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.calls, "no calls recorded")
	return r.calls[len(r.calls)-1]
}

// fakeCosignBinary creates a placeholder executable file so the os.Stat check
// in NewCosignVerifier succeeds. The contents never run because the runner is
// stubbed; we just need a real path on disk.
func fakeCosignBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "cosign")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	return bin
}

func TestNewCosignVerifier_RequiresBinary(t *testing.T) {
	_, err := NewCosignVerifier("", "", "", nil)
	require.ErrorIs(t, err, ErrCosignBinaryMissing)
}

func TestNewCosignVerifier_RequiresExistingBinary(t *testing.T) {
	_, err := NewCosignVerifier(filepath.Join(t.TempDir(), "nope"), "", "", nil)
	require.ErrorIs(t, err, ErrCosignBinaryMissing)
}

func TestNewCosignVerifier_FillsDefaults(t *testing.T) {
	bin := fakeCosignBinary(t)
	v, err := NewCosignVerifier(bin, "", "", nil)
	require.NoError(t, err)
	require.Equal(t, DefaultCosignIdentityRegexp, v.identity)
	require.Equal(t, DefaultCosignOIDCIssuer, v.issuer)
	require.NotNil(t, v.runner)
}

func TestCosignVerify_Success(t *testing.T) {
	bin := fakeCosignBinary(t)
	runner := &recordingRunner{respOut: []byte("Verified OK\n")}
	v, err := NewCosignVerifier(bin, "ident", "iss", runner)
	require.NoError(t, err)

	err = v.Verify(context.Background(), []byte("blob"), []byte("sig"), []byte("cert"))
	require.NoError(t, err)

	call := runner.lastCall(t)
	require.Equal(t, bin, call.name)
	require.Equal(t, "verify-blob", call.args[0])
	require.Contains(t, call.args, "--certificate-identity-regexp")
	require.Contains(t, call.args, "ident")
	require.Contains(t, call.args, "--certificate-oidc-issuer")
	require.Contains(t, call.args, "iss")
	// The last arg is the blob path; it should live inside an adept temp dir.
	blobArg := call.args[len(call.args)-1]
	require.Contains(t, blobArg, "adept-cosign-")
}

func TestCosignVerify_FailureWrapsOutput(t *testing.T) {
	bin := fakeCosignBinary(t)
	runner := &recordingRunner{
		respOut: []byte("error: certificate identity mismatch\nfailed\n"),
		respErr: errors.New("exit status 1"),
	}
	v, err := NewCosignVerifier(bin, "ident", "iss", runner)
	require.NoError(t, err)

	err = v.Verify(context.Background(), []byte("blob"), []byte("sig"), []byte("cert"))
	require.ErrorIs(t, err, ErrCosignVerifyFailed)
	require.Contains(t, err.Error(), "certificate identity mismatch")
}

func TestCosignVerify_RejectsEmptyInputs(t *testing.T) {
	bin := fakeCosignBinary(t)
	runner := &recordingRunner{}
	v, err := NewCosignVerifier(bin, "ident", "iss", runner)
	require.NoError(t, err)

	cases := []struct {
		name            string
		blob, sig, cert []byte
	}{
		{"empty blob", nil, []byte("s"), []byte("c")},
		{"empty sig", []byte("b"), nil, []byte("c")},
		{"empty cert", []byte("b"), []byte("s"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v.Verify(context.Background(), tc.blob, tc.sig, tc.cert)
			require.Error(t, err)
		})
	}
	runner.mu.Lock()
	require.Empty(t, runner.calls, "verifier should not invoke runner on empty inputs")
	runner.mu.Unlock()
}

func TestCosignVerify_NilReceiver(t *testing.T) {
	var v *CosignVerifier
	err := v.Verify(context.Background(), []byte("b"), []byte("s"), []byte("c"))
	require.Error(t, err)
}

func TestCosignVerify_TempFilesCleanedUp(t *testing.T) {
	bin := fakeCosignBinary(t)
	var seenDir string
	runner := stubRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		// The blob path is always the last positional arg.
		if len(args) > 0 {
			seenDir = filepath.Dir(args[len(args)-1])
		}
		return []byte("ok"), nil
	})
	v, err := NewCosignVerifier(bin, "ident", "iss", runner)
	require.NoError(t, err)
	require.NoError(t, v.Verify(context.Background(), []byte("b"), []byte("s"), []byte("c")))
	require.NotEmpty(t, seenDir)
	_, statErr := os.Stat(seenDir)
	require.True(t, os.IsNotExist(statErr), "temp dir should be removed after Verify")
}

func TestTrimOutput(t *testing.T) {
	require.Equal(t, "(no output)", trimOutput(nil))
	require.Equal(t, "(no output)", trimOutput([]byte("   \n\t")))
	require.Equal(t, "a | b", trimOutput([]byte("a\nb\n")))
	long := strings.Repeat("x", 1024)
	out := trimOutput([]byte(long))
	// 512 bytes + the trailing ellipsis rune (3 bytes in UTF-8).
	require.Len(t, out, 512+len("…"))
	require.True(t, strings.HasSuffix(out, "…"))
}

// stubRunnerFunc is a functional CommandRunner used by tests that need
// per-call assertions. It is safe for concurrent use because Go closures over
// captured vars are themselves safe when the caller uses a mutex; here we
// only ever invoke it serially from a single Verify call.
type stubRunnerFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

func (f stubRunnerFunc) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return f(ctx, name, args...)
}

func TestExecRunner_Run(t *testing.T) {
	// Smoke-test the production runner using a guaranteed-present binary.
	r := ExecRunner{}
	out, err := r.Run(context.Background(), "sh", "-c", "printf hello")
	require.NoError(t, err)
	require.Equal(t, "hello", string(out))
}

func TestExecRunner_NonZeroExit(t *testing.T) {
	r := ExecRunner{}
	_, err := r.Run(context.Background(), "sh", "-c", "echo nope >&2; exit 7")
	require.Error(t, err)
}

// Detect tests live in this file because they exercise the cosign wiring.

func TestDetect_DisabledReturnsNoop(t *testing.T) {
	res, err := Detect(DetectOptions{EnvOverride: "disabled"})
	require.NoError(t, err)
	require.Equal(t, BackendDisabled, res.Backend)
	require.NotNil(t, res.Verifier)
	require.NotNil(t, res.Signer)
}

func TestDetect_NoopExplicit(t *testing.T) {
	res, err := Detect(DetectOptions{EnvOverride: "noop"})
	require.NoError(t, err)
	require.Equal(t, BackendNoop, res.Backend)
}

func TestDetect_UnknownBackend(t *testing.T) {
	_, err := Detect(DetectOptions{EnvOverride: "frobnicate"})
	require.ErrorIs(t, err, ErrUnknownBackend)
}

func TestDetect_CosignExplicit_MissingBinary(t *testing.T) {
	res, err := Detect(DetectOptions{
		EnvOverride: "cosign",
		LookPath:    func(string) (string, error) { return "", errors.New("not on PATH") },
	})
	require.Nil(t, res)
	require.Error(t, err)
	require.Contains(t, err.Error(), "install cosign")
}

func TestDetect_CosignExplicit_BinaryPresent(t *testing.T) {
	bin := fakeCosignBinary(t)
	res, err := Detect(DetectOptions{
		EnvOverride: "cosign",
		LookPath:    func(string) (string, error) { return bin, nil },
		Identity:    "id",
		Issuer:      "iss",
	})
	require.NoError(t, err)
	require.Equal(t, BackendCosignKeyless, res.Backend)
	cv, ok := res.Verifier.(*CosignVerifier)
	require.True(t, ok, "expected *CosignVerifier, got %T", res.Verifier)
	require.Equal(t, "id", cv.identity)
	require.Equal(t, "iss", cv.issuer)
}

func TestDetect_AutoFallsBackToNoop(t *testing.T) {
	// Auto-detect with no cosign: expect Noop + notice.
	t.Setenv(EnvBackend, "")
	res, err := Detect(DetectOptions{
		LookPath: func(string) (string, error) { return "", errors.New("missing") },
	})
	require.NoError(t, err)
	require.Equal(t, BackendNoop, res.Backend)
	require.NotEmpty(t, res.Notice)
}

func TestDetect_AutoPicksCosignWhenAvailable(t *testing.T) {
	bin := fakeCosignBinary(t)
	t.Setenv(EnvBackend, "")
	res, err := Detect(DetectOptions{
		LookPath: func(string) (string, error) { return bin, nil },
	})
	require.NoError(t, err)
	require.Equal(t, BackendCosignKeyless, res.Backend)
	require.Empty(t, res.Notice)
}

func TestDetect_EnvOverrideHonored(t *testing.T) {
	// EnvOverride takes precedence over env var.
	t.Setenv(EnvBackend, "cosign")
	res, err := Detect(DetectOptions{EnvOverride: "noop"})
	require.NoError(t, err)
	require.Equal(t, BackendNoop, res.Backend)
}

func TestDetect_FallsBackToEnvWhenOverrideEmpty(t *testing.T) {
	t.Setenv(EnvBackend, "noop")
	res, err := Detect(DetectOptions{})
	require.NoError(t, err)
	require.Equal(t, BackendNoop, res.Backend)
}

func TestDetect_IdentityIssuerFromEnv(t *testing.T) {
	bin := fakeCosignBinary(t)
	t.Setenv(EnvCosignIdentity, "env-id")
	t.Setenv(EnvCosignIssuer, "env-iss")
	res, err := Detect(DetectOptions{
		EnvOverride: "cosign",
		LookPath:    func(string) (string, error) { return bin, nil },
	})
	require.NoError(t, err)
	cv := res.Verifier.(*CosignVerifier)
	require.Equal(t, "env-id", cv.identity)
	require.Equal(t, "env-iss", cv.issuer)
}

// helper to silence "imported and not used" if a future refactor drops a fmt usage.
var _ = fmt.Sprintf
