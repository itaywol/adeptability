package exchange

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/itaywol/adeptability/pkg/adept"
)

// HTTPDoer abstracts *http.Client.Do so tests inject a fake without a
// listener. Mirrors internal/org.HTTPDoer.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DefaultHTTPTimeout caps a single client call when the caller supplies no
// context deadline.
const DefaultHTTPTimeout = 15 * time.Second

// Client talks to a billboard server. The member token authenticates every
// call except Register (which presents the bootstrap token instead).
type Client struct {
	baseURL string
	token   string
	doer    HTTPDoer
}

// NewClient returns a Client for baseURL authenticating as token. A nil doer
// falls back to a default http.Client.
func NewClient(baseURL, token string, doer HTTPDoer) *Client {
	if doer == nil {
		doer = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, doer: doer}
}

// do issues one request. authToken overrides the client token (used by
// Register). out, when non-nil, receives the decoded JSON response.
func (c *Client) do(ctx context.Context, method, path, authToken string, body, out any) error {
	if c.baseURL == "" {
		return fmt.Errorf("exchange client: empty base URL")
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("exchange client: encode request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("exchange client: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	resp, err := c.doer.Do(req)
	if err != nil {
		return fmt.Errorf("exchange client: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.statusError(resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("exchange client: decode response: %w", err)
		}
	}
	return nil
}

// statusError maps an error response to a sentinel where one fits, so the CLI
// can present a friendly message and pick the right exit code.
func (c *Client) statusError(resp *http.Response) error {
	var payload struct {
		Error string `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = json.Unmarshal(raw, &payload)
	msg := payload.Error
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", adept.ErrExchangeUnauthorized, msg)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", adept.ErrExchangeForbidden, msg)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", adept.ErrExchangeItemNotFound, msg)
	case http.StatusConflict:
		return fmt.Errorf("%w: %s", adept.ErrExchangeHandleTaken, msg)
	default:
		return fmt.Errorf("exchange client: status %d: %s", resp.StatusCode, msg)
	}
}

// Register presents the bootstrap token and claims handle, returning a fresh
// member token.
func (c *Client) Register(ctx context.Context, bootstrap, handle string) (string, error) {
	var resp tokenResp
	if err := c.do(ctx, http.MethodPost, "/register", bootstrap, registerReq{Handle: handle}, &resp); err != nil {
		return "", err
	}
	return resp.Token, nil
}

// Rotate swaps the client's token for a fresh one and returns it.
func (c *Client) Rotate(ctx context.Context) (string, error) {
	var resp tokenResp
	if err := c.do(ctx, http.MethodPost, "/token/rotate", c.token, nil, &resp); err != nil {
		return "", err
	}
	return resp.Token, nil
}

// CreateItem posts a new request and returns the stored item.
func (c *Client) CreateItem(ctx context.Context, title, body string, assignees, tags []string) (adept.ExchangeItem, error) {
	var item adept.ExchangeItem
	err := c.do(ctx, http.MethodPost, "/items", c.token,
		createItemReq{Title: title, Body: body, Assignees: assignees, Tags: tags}, &item)
	return item, err
}

// ListItems returns items, optionally restricted to the caller and a status.
func (c *Client) ListItems(ctx context.Context, mine bool, status string) ([]adept.ExchangeItem, error) {
	q := url.Values{}
	if mine {
		q.Set("mine", "1")
	}
	if status != "" {
		q.Set("status", status)
	}
	path := "/items"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp itemsResp
	if err := c.do(ctx, http.MethodGet, path, c.token, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// GetItem returns a single item by id.
func (c *Client) GetItem(ctx context.Context, id int) (adept.ExchangeItem, error) {
	var item adept.ExchangeItem
	err := c.do(ctx, http.MethodGet, "/items/"+strconv.Itoa(id), c.token, nil, &item)
	return item, err
}

// AddComment posts a response on an item and returns the updated item.
func (c *Client) AddComment(ctx context.Context, id int, body string) (adept.ExchangeItem, error) {
	var item adept.ExchangeItem
	err := c.do(ctx, http.MethodPost, "/items/"+strconv.Itoa(id)+"/comments", c.token, commentReq{Body: body}, &item)
	return item, err
}

// SetStatus changes an item's status (author-only, enforced server-side).
func (c *Client) SetStatus(ctx context.Context, id int, status string) (adept.ExchangeItem, error) {
	var item adept.ExchangeItem
	err := c.do(ctx, http.MethodPatch, "/items/"+strconv.Itoa(id), c.token, statusReq{Status: status}, &item)
	return item, err
}
