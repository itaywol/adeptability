package exchange

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

// newTestServer wires a memory-backed server with a known bootstrap token and
// returns an unauthenticated client plus the bootstrap token.
func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	store, err := NewDriverRegistry().Open("memory", "")
	require.NoError(t, err)
	boot, err := EnsureBootstrap(store, false)
	require.NoError(t, err)
	srv := httptest.NewServer(NewServer(store))
	t.Cleanup(srv.Close)
	return srv, boot
}

func registerClient(t *testing.T, baseURL, boot, handle string) *Client {
	t.Helper()
	tok, err := NewClient(baseURL, "", nil).Register(context.Background(), boot, handle)
	require.NoError(t, err)
	return NewClient(baseURL, tok, nil)
}

func TestServer_RegisterAuth(t *testing.T) {
	srv, boot := newTestServer(t)
	ctx := context.Background()

	_, err := NewClient(srv.URL, "", nil).Register(ctx, "wrong-boot", "alice")
	require.ErrorIs(t, err, adept.ErrExchangeUnauthorized)

	tok, err := NewClient(srv.URL, "", nil).Register(ctx, boot, "alice")
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	_, err = NewClient(srv.URL, "", nil).Register(ctx, boot, "alice")
	require.ErrorIs(t, err, adept.ErrExchangeHandleTaken)
}

func TestServer_ItemLifecycle(t *testing.T) {
	srv, boot := newTestServer(t)
	ctx := context.Background()
	alice := registerClient(t, srv.URL, boot, "alice")
	bob := registerClient(t, srv.URL, boot, "bob")

	item, err := alice.CreateItem(ctx, "how does auth work", "details", []string{"bob"}, []string{"auth"})
	require.NoError(t, err)
	require.Equal(t, adept.ExchangeStatusAttention, item.Status)

	// bob sees it under --mine (he's an assignee) and responds.
	mine, err := bob.ListItems(ctx, true, "")
	require.NoError(t, err)
	require.Len(t, mine, 1)

	updated, err := bob.AddComment(ctx, item.ID, "use the middleware")
	require.NoError(t, err)
	require.Equal(t, adept.ExchangeStatusInProgress, updated.Status, "first response auto-flips")
	require.Len(t, updated.Comments, 1)

	// non-author cannot change status.
	_, err = bob.SetStatus(ctx, item.ID, adept.ExchangeStatusClosed)
	require.ErrorIs(t, err, adept.ErrExchangeForbidden)

	// author can close.
	closed, err := alice.SetStatus(ctx, item.ID, adept.ExchangeStatusClosed)
	require.NoError(t, err)
	require.Equal(t, adept.ExchangeStatusClosed, closed.Status)
}

func TestServer_AuthAndNotFound(t *testing.T) {
	srv, boot := newTestServer(t)
	ctx := context.Background()
	alice := registerClient(t, srv.URL, boot, "alice")

	_, err := NewClient(srv.URL, "bad-token", nil).ListItems(ctx, false, "")
	require.ErrorIs(t, err, adept.ErrExchangeUnauthorized)

	_, err = alice.GetItem(ctx, 999)
	require.ErrorIs(t, err, adept.ErrExchangeItemNotFound)
}

func TestServer_RotateInvalidatesOldToken(t *testing.T) {
	srv, boot := newTestServer(t)
	ctx := context.Background()
	tok, err := NewClient(srv.URL, "", nil).Register(ctx, boot, "alice")
	require.NoError(t, err)

	c := NewClient(srv.URL, tok, nil)
	newTok, err := c.Rotate(ctx)
	require.NoError(t, err)
	require.NotEqual(t, tok, newTok)

	// Old token no longer authenticates.
	_, err = NewClient(srv.URL, tok, nil).ListItems(ctx, false, "")
	require.ErrorIs(t, err, adept.ErrExchangeUnauthorized)
	// New token works.
	_, err = NewClient(srv.URL, newTok, nil).ListItems(ctx, false, "")
	require.NoError(t, err)
}
