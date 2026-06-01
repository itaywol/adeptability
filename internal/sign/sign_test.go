package sign

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoop_VerifyAlwaysSucceeds(t *testing.T) {
	v, _ := NewNoop()
	require.NoError(t, v.Verify(context.Background(), []byte("blob"), []byte("sig"), []byte("cert")))
	require.NoError(t, v.Verify(context.Background(), nil, nil, nil))
}

func TestNoop_SignReturnsEmpty(t *testing.T) {
	_, s := NewNoop()
	sig, cert, err := s.Sign(context.Background(), []byte("blob"))
	require.NoError(t, err)
	require.Nil(t, sig)
	require.Nil(t, cert)
}

func TestNoop_PairSatisfiesInterfaces(t *testing.T) {
	v, s := NewNoop()
	var _ Verifier = v
	var _ Signer = s
}
