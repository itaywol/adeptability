//go:build cosign_integration

package sign

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCosignVerify_Integration_FailureWithBogusInputs requires a real cosign
// binary on PATH. Build with `-tags cosign_integration` to enable it. The
// test asserts that random bytes do NOT verify against any real Sigstore
// transparency log entry — i.e. cosign exits non-zero and we surface that.
func TestCosignVerify_Integration_FailureWithBogusInputs(t *testing.T) {
	bin, err := exec.LookPath("cosign")
	if err != nil {
		t.Skip("cosign not on PATH; install cosign to run this integration test")
	}
	v, err := NewCosignVerifier(bin, DefaultCosignIdentityRegexp, DefaultCosignOIDCIssuer, nil)
	require.NoError(t, err)
	err = v.Verify(context.Background(),
		[]byte("not-a-real-blob"),
		[]byte("not-a-real-signature"),
		[]byte("not-a-real-certificate"),
	)
	require.Error(t, err, "bogus inputs must fail verification")
	require.ErrorIs(t, err, ErrCosignVerifyFailed)
}
