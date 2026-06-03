package github

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// zcovEntry is a single tar entry spec for the in-memory tarball builder
// used by the coverage tests. Named with a z-prefix so it never collides
// with helpers in client_regress_test.go.
type zcovEntry struct {
	name     string
	typeflag byte
	body     []byte
}

// zcovTarGz builds a gzipped tar mimicking a GitHub tarball: every path is
// nested under a top-level wrapper dir (e.g. "repo-abc123/..."). Unlike the
// regression helper it always writes a header Size equal to len(body), which
// is what the io.ReadFull/LimitReader read path expects on the happy paths.
func zcovTarGz(t *testing.T, entries []zcovEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Size:     int64(len(e.body)),
			Mode:     0o644,
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if len(e.body) > 0 {
			_, err := tw.Write(e.body)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// newTestClient wires a *client at the given httptest server so no real
// network is touched. Returned as the concrete type so tests can reach base.
func newTestClient(srv *httptest.Server) *client {
	return &client{http: srv.Client(), base: srv.URL}
}

// ---------------------------------------------------------------------------
// ExtractSkillDir
// ---------------------------------------------------------------------------

// Strips the top-level wrapper dir and, given multiple candidate paths,
// matches the deeper "skills/<name>" layout, keying files inner-relative.
func TestExtractSkillDir_StripsWrapperAndMatchesFullPath(t *testing.T) {
	tgz := zcovTarGz(t, []zcovEntry{
		{name: "repo-abc123/skills/find-skills/SKILL.md", typeflag: tar.TypeReg, body: []byte("# find skills")},
		{name: "repo-abc123/skills/find-skills/references/api.md", typeflag: tar.TypeReg, body: []byte("api body")},
	})
	files, matched, err := ExtractSkillDir(bytes.NewReader(tgz), "find-skills", []string{"find-skills", "skills/find-skills"})
	require.NoError(t, err)
	require.Equal(t, "skills/find-skills", matched)
	require.Len(t, files, 2)
	require.Equal(t, []byte("# find skills"), files["SKILL.md"])
	require.Equal(t, []byte("api body"), files["references/api.md"])
	// Wrapper + candidate prefix both stripped: no leftover path segments.
	for k := range files {
		require.False(t, strings.HasPrefix(k, "repo-abc123/"), "wrapper not stripped from %q", k)
		require.False(t, strings.HasPrefix(k, "skills/"), "candidate prefix not stripped from %q", k)
	}
}

// When the tarball contains files for two different candidate layouts, only
// the first layout to match wins; the other layout's files are dropped (the
// `matchedPath != matched -> continue` branch), so there is no contamination.
func TestExtractSkillDir_FirstLayoutWinsRejectsMixed(t *testing.T) {
	tgz := zcovTarGz(t, []zcovEntry{
		{name: "wrap/find-skills/SKILL.md", typeflag: tar.TypeReg, body: []byte("flat")},
		{name: "wrap/skills/find-skills/OTHER.md", typeflag: tar.TypeReg, body: []byte("nested")},
	})
	files, matched, err := ExtractSkillDir(bytes.NewReader(tgz), "find-skills", []string{"find-skills", "skills/find-skills"})
	require.NoError(t, err)
	// "find-skills" appears earlier in candidatePaths and matches the first
	// entry, so it claims the layout; the "skills/find-skills" entry is dropped.
	require.Equal(t, "find-skills", matched)
	require.Len(t, files, 1)
	require.Contains(t, files, "SKILL.md")
	require.NotContains(t, files, "OTHER.md")
}

// A directory entry under the matched prefix yields no map key, and a regular
// file's body is read at exactly hdr.Size bytes (catches partial-read / off-
// by-one regressions in the bounded read path).
func TestExtractSkillDir_SkipsDirEntriesAndUsesHdrSize(t *testing.T) {
	body := bytes.Repeat([]byte("A"), 4096) // multi-block body
	tgz := zcovTarGz(t, []zcovEntry{
		{name: "repo-x/find-skills/references", typeflag: tar.TypeDir, body: nil},
		{name: "repo-x/find-skills/references/big.txt", typeflag: tar.TypeReg, body: body},
		{name: "repo-x/find-skills/SKILL.md", typeflag: tar.TypeReg, body: []byte("# ok")},
	})
	files, matched, err := ExtractSkillDir(bytes.NewReader(tgz), "find-skills", []string{"find-skills"})
	require.NoError(t, err)
	require.Equal(t, "find-skills", matched)
	require.Len(t, files, 2)
	require.NotContains(t, files, "references") // dir entry produced no key
	require.Len(t, files["references/big.txt"], 4096)
	require.Equal(t, body, files["references/big.txt"])
	require.Equal(t, []byte("# ok"), files["SKILL.md"])
}

// When no entry lives under any candidate path, the not-found branch returns
// a nil map, an empty matchedPath, and an error naming the skill + candidates.
func TestExtractSkillDir_NotFoundError(t *testing.T) {
	tgz := zcovTarGz(t, []zcovEntry{
		{name: "repo-x/some-other-dir/README.md", typeflag: tar.TypeReg, body: []byte("nope")},
	})
	files, matched, err := ExtractSkillDir(bytes.NewReader(tgz), "find-skills", []string{"find-skills", "skills/find-skills"})
	require.Error(t, err)
	require.Nil(t, files)
	require.Empty(t, matched)
	require.Contains(t, err.Error(), "find-skills")
	require.Contains(t, err.Error(), "not found in tarball")
	require.Contains(t, err.Error(), "skills/find-skills")
}

// Non-gzip bytes hit the gzip.NewReader failure branch: wrapped "gunzip
// tarball" error and a nil map.
func TestExtractSkillDir_BadGzip(t *testing.T) {
	files, matched, err := ExtractSkillDir(bytes.NewReader([]byte("this is not gzip data at all")), "find-skills", []string{"find-skills"})
	require.Error(t, err)
	require.Nil(t, files)
	require.Empty(t, matched)
	require.Contains(t, err.Error(), "gunzip tarball")
}

// ---------------------------------------------------------------------------
// ResolveRef
// ---------------------------------------------------------------------------

// An explicit ref hits /commits/{ref} directly and decodes the sha, without
// any preceding RepoInfo call (single request).
func TestResolveRef_ExplicitRef(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		require.Equal(t, "/repos/o/r/commits/v1.2.3", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"sha":"deadbeef"}`)
	}))
	defer srv.Close()

	sha, err := newTestClient(srv).ResolveRef(context.Background(), "o", "r", "v1.2.3")
	require.NoError(t, err)
	require.Equal(t, "deadbeef", sha)
	require.Len(t, paths, 1, "explicit ref must not trigger a RepoInfo call")
}

// An empty ref first calls RepoInfo, uses default_branch in a second commits
// call, and returns the resolved sha.
func TestResolveRef_EmptyRefResolvesDefaultBranch(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/repos/o/r":
			_, _ = io.WriteString(w, `{"default_branch":"trunk"}`)
		case "/repos/o/r/commits/trunk":
			_, _ = io.WriteString(w, `{"sha":"abc"}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	sha, err := newTestClient(srv).ResolveRef(context.Background(), "o", "r", "")
	require.NoError(t, err)
	require.Equal(t, "abc", sha)
	require.Equal(t, []string{"/repos/o/r", "/repos/o/r/commits/trunk"}, paths)
}

// An empty ref where the repo has a blank default_branch falls back to the
// hardcoded "main".
func TestResolveRef_EmptyRefFallsBackToMain(t *testing.T) {
	var commitsPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r":
			_, _ = io.WriteString(w, `{"default_branch":""}`)
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/commits/"):
			commitsPath = r.URL.Path
			_, _ = io.WriteString(w, `{"sha":"mainsha"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	sha, err := newTestClient(srv).ResolveRef(context.Background(), "o", "r", "")
	require.NoError(t, err)
	require.Equal(t, "mainsha", sha)
	require.Equal(t, "/repos/o/r/commits/main", commitsPath)
}

// Non-200 from the commits endpoint and an empty-sha 200 both surface as
// descriptive errors.
func TestResolveRef_Non200AndEmptySha(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantErr  string
		wantSHA0 bool
	}{
		{name: "404", status: http.StatusNotFound, body: `{}`, wantErr: "http 404"},
		{name: "empty sha", status: http.StatusOK, body: `{"sha":""}`, wantErr: "empty sha in response"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			sha, err := newTestClient(srv).ResolveRef(context.Background(), "o", "r", "v1")
			require.Error(t, err)
			require.Empty(t, sha)
			require.Contains(t, err.Error(), tc.wantErr)
			require.Contains(t, err.Error(), "o/r@v1")
		})
	}
}

// A malformed JSON body on a 200 hits the decode-error branch.
func TestResolveRef_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{not json`)
	}))
	defer srv.Close()

	sha, err := newTestClient(srv).ResolveRef(context.Background(), "o", "r", "v1")
	require.Error(t, err)
	require.Empty(t, sha)
	require.Contains(t, err.Error(), "decode github commit")
}

// Invalid owner/repo and refs are rejected before any HTTP call. A failing
// server confirms no request is made.
func TestResolveRef_ValidationRejectsBeforeHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("HTTP must not be called for invalid input, got %q", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newTestClient(srv)

	_, err := c.ResolveRef(context.Background(), "ow/ner", "r", "v1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid repo owner")

	_, err = c.ResolveRef(context.Background(), "o", "r", "../escape")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid ref")
}

// ---------------------------------------------------------------------------
// RepoInfo
// ---------------------------------------------------------------------------

// Parses scalar fields and flattens + trims the nested license SPDX id.
func TestRepoInfo_ParsesNestedLicenseAndTrims(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/repos/o/r", r.URL.Path)
		_, _ = io.WriteString(w, `{"full_name":"o/r","stargazers_count":42,"default_branch":"main","license":{"spdx_id":"  MIT  "}}`)
	}))
	defer srv.Close()

	meta, err := newTestClient(srv).RepoInfo(context.Background(), "o", "r")
	require.NoError(t, err)
	require.Equal(t, "o/r", meta.FullName)
	require.Equal(t, 42, meta.Stars)
	require.Equal(t, "main", meta.DefaultBranch)
	require.Equal(t, "MIT", meta.License) // nested + whitespace-trimmed
}

// Non-200 returns a zero RepoMeta and a descriptive error.
func TestRepoInfo_Non200Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"forbidden"}`)
	}))
	defer srv.Close()

	meta, err := newTestClient(srv).RepoInfo(context.Background(), "o", "r")
	require.Error(t, err)
	require.Equal(t, RepoMeta{}, meta)
	require.Contains(t, err.Error(), "github repo info o/r: http 403")
}

// Malformed JSON hits the decode-error branch and returns a zero RepoMeta.
func TestRepoInfo_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `not-json`)
	}))
	defer srv.Close()

	meta, err := newTestClient(srv).RepoInfo(context.Background(), "o", "r")
	require.Error(t, err)
	require.Equal(t, RepoMeta{}, meta)
	require.Contains(t, err.Error(), "decode github repo")
}

// ---------------------------------------------------------------------------
// FetchTarball
// ---------------------------------------------------------------------------

// Non-200 closes the body and returns an http-status error with a nil body.
func TestFetchTarball_Non200ClosesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/repos/o/r/tarball/deadbeef", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()

	rc, err := newTestClient(srv).FetchTarball(context.Background(), "o", "r", "deadbeef")
	require.Error(t, err)
	require.Nil(t, rc)
	require.Contains(t, err.Error(), "http 500")
	require.Contains(t, err.Error(), "o/r@deadbeef")
}

// Happy path returns the response body unmodified for the caller to read +
// close (caller-closes contract).
func TestFetchTarball_HappyPathReturnsBody(t *testing.T) {
	payload := []byte("\x1f\x8b\x08 fake tarball bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	rc, err := newTestClient(srv).FetchTarball(context.Background(), "o", "r", "deadbeef")
	require.NoError(t, err)
	require.NotNil(t, rc)
	defer rc.Close()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

// Invalid sha is rejected before any HTTP call.
func TestFetchTarball_ValidationRejectsBeforeHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("HTTP must not be called for invalid sha, got %q", r.URL.Path)
	}))
	defer srv.Close()

	rc, err := newTestClient(srv).FetchTarball(context.Background(), "o", "r", "../evil")
	require.Error(t, err)
	require.Nil(t, rc)
	require.Contains(t, err.Error(), "invalid ref")
}

// ---------------------------------------------------------------------------
// authedRequest header / token injection (exercised through a real method)
// ---------------------------------------------------------------------------

func TestAuthedRequest_HeadersAndTokenInjection(t *testing.T) {
	tests := []struct {
		name      string
		setToken  bool
		token     string
		wantAuth  string
		wantNoAth bool
	}{
		{name: "token set", setToken: true, token: "s3cr3t", wantAuth: "Bearer s3cr3t"},
		{name: "token unset", setToken: false, wantNoAth: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Ensure a deterministic env regardless of host state.
			if tc.setToken {
				t.Setenv("GITHUB_TOKEN", tc.token)
			} else {
				t.Setenv("GITHUB_TOKEN", "")
			}
			var gotHeaders http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHeaders = r.Header.Clone()
				_, _ = io.WriteString(w, `{"sha":"x"}`)
			}))
			defer srv.Close()

			_, err := newTestClient(srv).ResolveRef(context.Background(), "o", "r", "v1")
			require.NoError(t, err)
			require.Equal(t, "application/vnd.github+json", gotHeaders.Get("Accept"))
			require.Equal(t, "2022-11-28", gotHeaders.Get("X-GitHub-Api-Version"))
			require.Equal(t, "adept-cli", gotHeaders.Get("User-Agent"))
			if tc.wantNoAth {
				require.Empty(t, gotHeaders.Get("Authorization"))
			} else {
				require.Equal(t, tc.wantAuth, gotHeaders.Get("Authorization"))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// New
// ---------------------------------------------------------------------------

// New(nil) falls back to http.DefaultClient and the public base URL without
// panicking; New(custom) preserves the passed client.
func TestNew_NilClientUsesDefault(t *testing.T) {
	c, ok := New(nil).(*client)
	require.True(t, ok)
	require.NotNil(t, c)
	require.Same(t, http.DefaultClient, c.http)
	require.Equal(t, "https://api.github.com", c.base)

	custom := &http.Client{}
	c2, ok := New(custom).(*client)
	require.True(t, ok)
	require.Same(t, custom, c2.http)
}
