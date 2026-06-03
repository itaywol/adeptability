package skillssh

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// roundTripFunc is a tiny http.RoundTripper fake so we can simulate
// transport-layer failures without touching the network.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHit_IsGitHubSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{"owner/repo", "octocat/hello", true},
		{"domain with dot", "skills.volces.com", false},
		{"two slashes", "owner/repo/skill", false},
		{"no slash", "plainname", false},
		{"empty", "", false},
		{"one slash but dotted", "owner/repo.git", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := Hit{Source: tc.source}
			require.Equal(t, tc.want, h.IsGitHubSource())
		})
	}
}

func TestSearch_Success(t *testing.T) {
	var gotPath, gotQuery, gotAccept, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("q")
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"skills":[{"id":"a/b/c","skillId":"c","name":"C","installs":42,"source":"a/b"}]}`))
	}))
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	hits, err := c.Search(context.Background(), "hello")
	require.NoError(t, err)
	require.Len(t, hits, 1)

	got := hits[0]
	require.Equal(t, "a/b/c", got.ID)
	require.Equal(t, "c", got.SkillID)
	require.Equal(t, "C", got.Name)
	require.Equal(t, 42, got.Installs)
	require.Equal(t, "a/b", got.Source)
	require.True(t, got.IsGitHubSource())

	// Request contract the server side relies on.
	require.Equal(t, "/api/search", gotPath)
	require.Equal(t, "hello", gotQuery)
	require.Equal(t, "application/json", gotAccept)
	require.Equal(t, "adept-cli", gotUA)
}

func TestSearch_EmptyQuery(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	hits, err := c.Search(context.Background(), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty query")
	require.Nil(t, hits)
	require.False(t, called, "guard must short-circuit before any HTTP request")
}

func TestSearch_QueryEscaping(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"spaces and ampersand", "go & rust"},
		{"reserved chars", "a=b?c#d/e"},
		{"unicode", "café ☕"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query().Get("q")
				_, _ = w.Write([]byte(`{"skills":[]}`))
			}))
			defer srv.Close()

			c := New(srv.Client(), srv.URL)
			_, err := c.Search(context.Background(), tc.query)
			require.NoError(t, err)
			// The server must decode the exact original string back out,
			// proving url.QueryEscape round-trips correctly.
			require.Equal(t, tc.query, gotQuery)
		})
	}
}

func TestSearch_Non200(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"internal server error", http.StatusInternalServerError},
		{"not found", http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
			}))
			defer srv.Close()

			c := New(srv.Client(), srv.URL)
			hits, err := c.Search(context.Background(), "boom")
			require.Error(t, err)
			require.Nil(t, hits)
			// Error must mention both the status code and the query.
			require.Contains(t, err.Error(), fmt.Sprintf("http %d", tc.code))
			require.Contains(t, err.Error(), "boom")
		})
	}
}

func TestSearch_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json{"))
	}))
	defer srv.Close()

	c := New(srv.Client(), srv.URL)
	hits, err := c.Search(context.Background(), "q")
	require.Error(t, err)
	require.Nil(t, hits)
	require.Contains(t, err.Error(), "decode skills.sh response")
}

func TestSearch_EmptyAndMissingEnvelope(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty skills array", `{"skills":[]}`},
		{"missing skills key", `{}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := New(srv.Client(), srv.URL)
			hits, err := c.Search(context.Background(), "q")
			require.NoError(t, err, "a missing/empty envelope must not be treated as failure")
			require.Empty(t, hits)
		})
	}
}

func TestSearch_TransportError(t *testing.T) {
	t.Run("closed server", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		base := srv.URL
		hcClient := srv.Client()
		srv.Close() // close before the request so Do fails at the transport layer

		c := New(hcClient, base)
		hits, err := c.Search(context.Background(), "q")
		require.Error(t, err)
		require.Nil(t, hits)
		require.Contains(t, err.Error(), "skills.sh search:")
	})

	t.Run("roundtripper failure", func(t *testing.T) {
		hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("dial refused")
		})}
		c := New(hc, "http://example.invalid")
		hits, err := c.Search(context.Background(), "q")
		require.Error(t, err)
		require.Nil(t, hits)
		require.Contains(t, err.Error(), "skills.sh search:")
		require.Contains(t, err.Error(), "dial refused")
	})
}

func TestSearch_ContextCancelled(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // block until the test releases us, after cancellation
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	c := New(srv.Client(), srv.URL)
	hits, err := c.Search(ctx, "q")
	require.Error(t, err, "cancellation must surface as an error, not hang")
	require.Nil(t, hits)
}

func TestNew_Defaults(t *testing.T) {
	t.Run("nil client and empty base yield a usable client", func(t *testing.T) {
		c := New(nil, "")
		require.NotNil(t, c)
	})

	t.Run("trailing slash trimmed so path has no double slash", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = w.Write([]byte(`{"skills":[]}`))
		}))
		defer srv.Close()

		// Base with a trailing slash must be trimmed; otherwise the path
		// would come through as "//api/search".
		c := New(srv.Client(), srv.URL+"/")
		_, err := c.Search(context.Background(), "q")
		require.NoError(t, err)
		require.Equal(t, "/api/search", gotPath)
	})
}
