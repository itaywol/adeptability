package org

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPDoer abstracts the *http.Client.Do signature so tests can inject a
// mock without spinning a real listener. A package-level default is
// avoided — wiring goes through NewHTTPClient explicitly.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DefaultHTTPTimeout caps a single Fetch when the caller does not pass a
// context deadline. Conservative enough to surface flaky networks fast
// without aborting legitimate slow handshakes.
const DefaultHTTPTimeout = 15 * time.Second

// httpClient fetches an org manifest over HTTP/HTTPS with ETag caching.
type httpClient struct {
	baseURL string
	parser  Parser
	doer    HTTPDoer
	cache   ETagCache
}

// NewHTTPClient returns a Client that GETs <baseURL>/org.yaml. If cache is
// non-nil, subsequent requests issue If-None-Match using the prior ETag and
// reuse the cached body on 304.
//
// baseURL is canonicalized: any trailing slash is stripped so callers can
// pass either "https://x/repo" or "https://x/repo/".
func NewHTTPClient(baseURL string, parser Parser, doer HTTPDoer, cache ETagCache) Client {
	return &httpClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		parser:  parser,
		doer:    doer,
		cache:   cache,
	}
}

// orgYAMLURL returns the canonical URL to fetch. Exposed indirectly via the
// Fetch contract; kept as a method so subclasses can extend later.
func (c *httpClient) orgYAMLURL() string {
	return c.baseURL + "/org.yaml"
}

// Fetch implements Client.Fetch with the ETag-aware behavior documented on
// NewHTTPClient.
func (c *httpClient) Fetch(ctx context.Context) (*Manifest, error) {
	if c.baseURL == "" {
		return nil, errors.New("org http client: empty base URL")
	}
	if c.parser == nil {
		return nil, errors.New("org http client: nil parser")
	}
	if c.doer == nil {
		return nil, errors.New("org http client: nil doer")
	}

	// Default deadline if caller didn't impose one.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultHTTPTimeout)
		defer cancel()
	}

	url := c.orgYAMLURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("org http client: build request: %w", err)
	}
	req.Header.Set("Accept", "application/yaml, text/yaml, text/plain;q=0.5")

	var cachedETag string
	var cachedBody []byte
	if c.cache != nil {
		if etag, body, ok := c.cache.Get(url); ok {
			cachedETag = etag
			cachedBody = body
			if etag != "" {
				req.Header.Set("If-None-Match", etag)
			}
		}
	}

	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("org http client: GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("org http client: read body: %w", err)
		}
		m, err := c.parser.Parse(body)
		if err != nil {
			return nil, fmt.Errorf("org http client: parse %s: %w", url, err)
		}
		if c.cache != nil {
			if etag := resp.Header.Get("ETag"); etag != "" {
				if cerr := c.cache.Put(url, etag, body); cerr != nil {
					// Cache write failures are non-fatal — surface as a
					// wrapped error only if the caller asked for it via a
					// future option. For now, swallow.
					_ = cerr
				}
			}
		}
		return m, nil

	case http.StatusNotModified:
		if len(cachedBody) == 0 {
			return nil, fmt.Errorf("org http client: 304 Not Modified but no cached body for %s", url)
		}
		m, err := c.parser.Parse(cachedBody)
		if err != nil {
			return nil, fmt.Errorf("org http client: parse cached %s: %w", url, err)
		}
		// Refresh ETag if the server sent one; keep body unchanged.
		if c.cache != nil {
			if etag := resp.Header.Get("ETag"); etag != "" && etag != cachedETag {
				_ = c.cache.Put(url, etag, cachedBody)
			}
		}
		return m, nil

	default:
		// Drain enough of the body to surface a useful diagnostic without
		// holding the connection open indefinitely.
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("org http client: GET %s: status %d: %s", url, resp.StatusCode, strings.TrimSpace(string(preview)))
	}
}
