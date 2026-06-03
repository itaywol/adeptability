package org

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const validManifest = "version: 1\nname: acme\nskills:\n  required:\n    - id: skill-a\n"

// recordingDoer captures every request and serves canned responses from a
// queue. If the queue is empty it returns the fallback response.
type recordingDoer struct {
	mu       sync.Mutex
	requests []*http.Request
	queue    []*http.Response
	queueErr []error
	fallback *http.Response
}

func (d *recordingDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Capture a shallow snapshot of the request (the body is nil for GETs).
	d.requests = append(d.requests, req)
	if len(d.queue) == 0 {
		if d.fallback != nil {
			return d.fallback, nil
		}
		return nil, errors.New("no canned response")
	}
	resp := d.queue[0]
	err := d.queueErr[0]
	d.queue = d.queue[1:]
	d.queueErr = d.queueErr[1:]
	return resp, err
}

func (d *recordingDoer) enqueue(resp *http.Response, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// The queued response is owned by the caller, which defers resp.Body.Close()
	// at the call site; closing it here would be premature. The bodyclose linter
	// cannot trace closure through the queue, so suppress the false positive.
	d.queue = append(d.queue, resp) //nolint:bodyclose // closed by the caller
	d.queueErr = append(d.queueErr, err)
}

func (d *recordingDoer) lastRequest(t *testing.T) *http.Request {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	require.NotEmpty(t, d.requests)
	return d.requests[len(d.requests)-1]
}

func cannedResponse(status int, body string, headers map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newTestParser(t *testing.T) Parser {
	t.Helper()
	p, err := NewParser()
	require.NoError(t, err)
	return p
}

func TestHTTPClient_FetchSuccess(t *testing.T) {
	parser := newTestParser(t)
	cache := NewFileETagCache(t.TempDir())
	doer := &recordingDoer{}
	resp := cannedResponse(200, validManifest, map[string]string{"ETag": `"v1"`})
	defer resp.Body.Close()
	doer.enqueue(resp, nil)

	c := NewHTTPClient("https://example.com/org/", parser, doer, cache)
	m, err := c.Fetch(context.Background())
	require.NoError(t, err)
	require.Equal(t, "acme", m.Name)

	req := doer.lastRequest(t)
	require.Equal(t, "GET", req.Method)
	require.Equal(t, "https://example.com/org/org.yaml", req.URL.String())
	require.Empty(t, req.Header.Get("If-None-Match"), "first fetch must not send If-None-Match")

	// Cache should have been populated.
	etag, body, ok := cache.Get("https://example.com/org/org.yaml")
	require.True(t, ok)
	require.Equal(t, `"v1"`, etag)
	require.Equal(t, validManifest, string(body))
}

func TestHTTPClient_FetchUses304CachedBody(t *testing.T) {
	parser := newTestParser(t)
	cache := NewFileETagCache(t.TempDir())
	doer := &recordingDoer{}
	// First call: 200 + ETag.
	resp200 := cannedResponse(200, validManifest, map[string]string{"ETag": `"v1"`})
	defer resp200.Body.Close()
	doer.enqueue(resp200, nil)
	// Second call: 304 (no body).
	resp304 := cannedResponse(304, "", map[string]string{"ETag": `"v1"`})
	defer resp304.Body.Close()
	doer.enqueue(resp304, nil)

	c := NewHTTPClient("https://example.com/org", parser, doer, cache)
	_, err := c.Fetch(context.Background())
	require.NoError(t, err)

	m2, err := c.Fetch(context.Background())
	require.NoError(t, err)
	require.Equal(t, "acme", m2.Name)

	req := doer.lastRequest(t)
	require.Equal(t, `"v1"`, req.Header.Get("If-None-Match"))
}

func TestHTTPClient_304WithoutCacheBodyFails(t *testing.T) {
	parser := newTestParser(t)
	doer := &recordingDoer{}
	resp := cannedResponse(304, "", nil)
	defer resp.Body.Close()
	doer.enqueue(resp, nil)

	// No cache → cannot satisfy 304.
	c := NewHTTPClient("https://example.com/org", parser, doer, nil)
	_, err := c.Fetch(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "304")
}

func TestHTTPClient_ParseError(t *testing.T) {
	parser := newTestParser(t)
	doer := &recordingDoer{}
	resp := cannedResponse(200, "not: : yaml", nil)
	defer resp.Body.Close()
	doer.enqueue(resp, nil)

	c := NewHTTPClient("https://example.com/org", parser, doer, nil)
	_, err := c.Fetch(context.Background())
	require.Error(t, err)
}

func TestHTTPClient_NetworkError(t *testing.T) {
	parser := newTestParser(t)
	doer := &recordingDoer{}
	doer.enqueue(nil, errors.New("dial tcp: connection refused"))

	c := NewHTTPClient("https://example.com/org", parser, doer, nil)
	_, err := c.Fetch(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "connection refused")
}

func TestHTTPClient_Non2xxStatus(t *testing.T) {
	parser := newTestParser(t)
	doer := &recordingDoer{}
	resp := cannedResponse(404, "no such org", nil)
	defer resp.Body.Close()
	doer.enqueue(resp, nil)

	c := NewHTTPClient("https://example.com/org", parser, doer, nil)
	_, err := c.Fetch(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
	require.Contains(t, err.Error(), "no such org")
}

func TestHTTPClient_EmptyBaseURL(t *testing.T) {
	parser := newTestParser(t)
	doer := &recordingDoer{}
	c := NewHTTPClient("", parser, doer, nil)
	_, err := c.Fetch(context.Background())
	require.Error(t, err)
}

func TestHTTPClient_NilParser(t *testing.T) {
	doer := &recordingDoer{}
	c := NewHTTPClient("https://example.com", nil, doer, nil)
	_, err := c.Fetch(context.Background())
	require.Error(t, err)
}

func TestHTTPClient_NilDoer(t *testing.T) {
	parser := newTestParser(t)
	c := NewHTTPClient("https://example.com", parser, nil, nil)
	_, err := c.Fetch(context.Background())
	require.Error(t, err)
}

func TestHTTPClient_AppliesDefaultTimeout(t *testing.T) {
	parser := newTestParser(t)
	doer := &recordingDoer{}
	resp := cannedResponse(200, validManifest, nil)
	defer resp.Body.Close()
	doer.enqueue(resp, nil)

	c := NewHTTPClient("https://example.com/org", parser, doer, nil)
	// Caller context has no deadline.
	_, err := c.Fetch(context.Background())
	require.NoError(t, err)

	req := doer.lastRequest(t)
	deadline, ok := req.Context().Deadline()
	require.True(t, ok, "Fetch should inject a default deadline when caller has none")
	require.WithinDuration(t, time.Now().Add(DefaultHTTPTimeout), deadline, DefaultHTTPTimeout)
}

func TestHTTPClient_CachePutFailureSwallowed(t *testing.T) {
	parser := newTestParser(t)
	doer := &recordingDoer{}
	resp := cannedResponse(200, validManifest, map[string]string{"ETag": `"v1"`})
	defer resp.Body.Close()
	doer.enqueue(resp, nil)

	cache := &failingCache{}
	c := NewHTTPClient("https://example.com/org", parser, doer, cache)
	m, err := c.Fetch(context.Background())
	require.NoError(t, err, "cache write errors must not fail the fetch")
	require.Equal(t, "acme", m.Name)
}

// failingCache always errors on Put and never hits on Get.
type failingCache struct{}

func (failingCache) Get(string) (string, []byte, bool) { return "", nil, false }
func (failingCache) Put(string, string, []byte) error  { return errors.New("disk full") }
