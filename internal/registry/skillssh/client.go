// Package skillssh talks to skills.sh — the only documented endpoint at
// the time of writing is GET /api/search?q=<query>, which returns a JSON
// list of skills with their install counts and source repos.
//
// adept treats skills.sh as the reputation oracle: it surfaces install
// count and source attribution. The actual SKILL.md content always
// comes from GitHub via internal/registry/github — skills.sh is not a
// content host.
package skillssh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Client wraps the small skills.sh surface. Constructed with the base
// URL so tests can point at a httptest server.
type Client interface {
	Search(ctx context.Context, query string) ([]Hit, error)
}

// Hit mirrors one element of the /api/search response payload. Source
// is "<owner>/<repo>" for GitHub-backed catalogs and a bare domain
// (e.g. "skills.volces.com") for hosted catalogs we cannot fetch from.
type Hit struct {
	ID       string `json:"id"`
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Installs int    `json:"installs"`
	Source   string `json:"source"`
}

// IsGitHubSource is true when Source looks like "<owner>/<repo>" (no
// dots, two segments). Used to filter the search results down to skills
// adept can actually install.
func (h Hit) IsGitHubSource() bool {
	if strings.Contains(h.Source, ".") {
		return false
	}
	return strings.Count(h.Source, "/") == 1
}

// New constructs a Client. Pass "" for base to use the public endpoint.
func New(hc *http.Client, base string) Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	if base == "" {
		base = "https://www.skills.sh"
	}
	return &client{http: hc, base: strings.TrimRight(base, "/")}
}

type client struct {
	http *http.Client
	base string
}

func (c *client) Search(ctx context.Context, query string) ([]Hit, error) {
	if query == "" {
		return nil, fmt.Errorf("skills.sh search: empty query")
	}
	u := fmt.Sprintf("%s/api/search?q=%s", c.base, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "adept-cli")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("skills.sh search: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("skills.sh search %q: http %d", query, res.StatusCode)
	}
	var payload struct {
		Skills []Hit `json:"skills"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode skills.sh response: %w", err)
	}
	return payload.Skills, nil
}
