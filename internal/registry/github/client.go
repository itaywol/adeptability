// Package github is a slim GitHub REST client for the skills.sh /
// `adept skill install` flow. We avoid the official go-github dep — we
// only need three operations:
//
//   1. Resolve a branch/tag to a commit SHA          (skill install)
//   2. Fetch a tarball of the repo at a specific SHA (skill install)
//   3. Read repo metadata (stars, license, default)  (skill info)
//
// All calls are unauthenticated by default. When $GITHUB_TOKEN is set we
// include it as a bearer header so private repos and higher rate limits
// just work without explicit adept config.
package github

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client wraps the small surface of the GitHub REST API we need.
type Client interface {
	// ResolveRef returns the commit SHA the branch/tag/full-sha currently
	// points to. An empty ref defaults to the repo's default branch.
	ResolveRef(ctx context.Context, owner, repo, ref string) (sha string, err error)
	// FetchTarball returns the gzipped tar payload of the repo at sha.
	// The caller is responsible for closing the returned ReadCloser.
	FetchTarball(ctx context.Context, owner, repo, sha string) (io.ReadCloser, error)
	// RepoInfo returns metadata used by `skill info`.
	RepoInfo(ctx context.Context, owner, repo string) (RepoMeta, error)
}

// RepoMeta is the subset of GitHub repo metadata adept surfaces in
// `skill info` / `skill install` previews. All fields are best-effort —
// a missing field stays at its zero value rather than erroring the whole
// request.
type RepoMeta struct {
	FullName      string    `json:"full_name"`
	HTMLURL       string    `json:"html_url"`
	Description   string    `json:"description"`
	DefaultBranch string    `json:"default_branch"`
	Stars         int       `json:"stargazers_count"`
	Forks         int       `json:"forks_count"`
	OpenIssues    int       `json:"open_issues_count"`
	License       string    `json:"-"`
	PushedAt      time.Time `json:"pushed_at"`
}

// New constructs a Client backed by http.DefaultClient and the public
// REST endpoint. A custom transport can be wired by passing it in.
func New(hc *http.Client) Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &client{
		http: hc,
		base: "https://api.github.com",
	}
}

type client struct {
	http *http.Client
	base string
}

// authedRequest attaches the optional GITHUB_TOKEN bearer + the
// recommended Accept and User-Agent headers.
func (c *client) authedRequest(ctx context.Context, method, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "adept-cli")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return req, nil
}

// ResolveRef hits /repos/{owner}/{repo}/commits/{ref} which accepts
// branches, tags, and SHA prefixes — returning the canonical SHA in the
// payload. When ref is empty we first pull the repo's default_branch.
func (c *client) ResolveRef(ctx context.Context, owner, repo, ref string) (string, error) {
	if ref == "" {
		meta, err := c.RepoInfo(ctx, owner, repo)
		if err != nil {
			return "", err
		}
		ref = meta.DefaultBranch
		if ref == "" {
			ref = "main"
		}
	}
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s", c.base, owner, repo, ref)
	req, err := c.authedRequest(ctx, http.MethodGet, url)
	if err != nil {
		return "", err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("github resolve ref: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github resolve ref %s/%s@%s: http %d", owner, repo, ref, res.StatusCode)
	}
	var payload struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode github commit: %w", err)
	}
	if payload.SHA == "" {
		return "", fmt.Errorf("github resolve ref %s/%s@%s: empty sha in response", owner, repo, ref)
	}
	return payload.SHA, nil
}

// FetchTarball pulls /repos/{owner}/{repo}/tarball/{sha}. The response is
// a gzipped tar where every entry is prefixed with a repo-name+sha
// directory; ExtractSkillDir below strips that prefix.
func (c *client) FetchTarball(ctx context.Context, owner, repo, sha string) (io.ReadCloser, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/tarball/%s", c.base, owner, repo, sha)
	req, err := c.authedRequest(ctx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github tarball: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("github tarball %s/%s@%s: http %d", owner, repo, sha, res.StatusCode)
	}
	return res.Body, nil
}

// RepoInfo wraps /repos/{owner}/{repo} plus a follow-up /license call so
// we can show the license name in `skill info`.
func (c *client) RepoInfo(ctx context.Context, owner, repo string) (RepoMeta, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", c.base, owner, repo)
	req, err := c.authedRequest(ctx, http.MethodGet, url)
	if err != nil {
		return RepoMeta{}, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return RepoMeta{}, fmt.Errorf("github repo info: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return RepoMeta{}, fmt.Errorf("github repo info %s/%s: http %d", owner, repo, res.StatusCode)
	}
	var meta RepoMeta
	// Embed license parsing inline — GitHub returns it nested as
	// {"license": {"spdx_id": "MIT"}}; the typed RepoMeta would ignore.
	raw := struct {
		RepoMeta
		License struct {
			SPDXID string `json:"spdx_id"`
		} `json:"license"`
	}{}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return RepoMeta{}, fmt.Errorf("decode github repo: %w", err)
	}
	meta = raw.RepoMeta
	meta.License = strings.TrimSpace(raw.License.SPDXID)
	return meta, nil
}

// ExtractSkillDir scans a gzipped GitHub tarball for the requested skill
// directory. GitHub wraps the repo content in a top-level directory like
// `vercel-labs-skills-abc1234/`, so we strip the first segment and then
// match against either:
//
//   - <skillPath>/...   (when skillPath is the full path inside the repo)
//   - <skillName>/...   (legacy)
//   - skills/<skillName>/...   (common convention in catalog repos)
//
// The first matching layout wins. The function returns a map keyed by
// path-inside-skill (e.g. "SKILL.md", "references/api.md") so the caller
// can write files relative to <project>/.adeptability/skills/<id>/.
func ExtractSkillDir(r io.Reader, skillName string, candidatePaths []string) (files map[string][]byte, matchedPath string, err error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, "", fmt.Errorf("gunzip tarball: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files = map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("read tarball entry: %w", err)
		}
		// Strip the top-level wrapper dir.
		rel := hdr.Name
		if i := strings.Index(rel, "/"); i >= 0 {
			rel = rel[i+1:]
		}
		if rel == "" {
			continue
		}
		// Find which candidate path this file belongs under (if any).
		matched := ""
		var inner string
		for _, cand := range candidatePaths {
			prefix := strings.Trim(cand, "/") + "/"
			if strings.HasPrefix(rel, prefix) {
				matched = strings.Trim(cand, "/")
				inner = strings.TrimPrefix(rel, prefix)
				break
			}
		}
		if matched == "" {
			continue
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		// First match wins per layout; reject mixed layouts.
		if matchedPath != "" && matchedPath != matched {
			continue
		}
		matchedPath = matched
		// Read the file body — bounded by tar entry size.
		buf := make([]byte, hdr.Size)
		if _, err := io.ReadFull(tr, buf); err != nil {
			return nil, "", fmt.Errorf("read tarball file %s: %w", hdr.Name, err)
		}
		files[inner] = buf
	}
	if matchedPath == "" || len(files) == 0 {
		return nil, "", fmt.Errorf("skill %q not found in tarball (tried: %v)", skillName, candidatePaths)
	}
	return files, matchedPath, nil
}
