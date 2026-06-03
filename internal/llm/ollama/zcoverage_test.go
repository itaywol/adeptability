package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/itaywol/adeptability/internal/llm"
	"github.com/stretchr/testify/require"
)

// newCapturingServer returns an httptest server whose handler records the
// last request's method, path, and decoded JSON body, and replies with the
// supplied status/body. The captured fields are guarded so the test can read
// them after Evaluate/Available returns.
func newCapturingServer(t *testing.T, status int, respBody string) (*httptest.Server, func() (method, path string, body map[string]any)) {
	t.Helper()
	var (
		mu     sync.Mutex
		method string
		path   string
		body   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		method = r.Method
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	read := func() (string, string, map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		return method, path, body
	}
	return srv, read
}

func TestNew_DefaultsAndOverrides(t *testing.T) {
	t.Run("nil client and empty fields fall back", func(t *testing.T) {
		p := New(nil, "", "")
		require.NotNil(t, p)
		require.NotNil(t, p.http, "nil *http.Client must get a default client")
		require.Equal(t, 60*time.Second, p.http.Timeout, "default client uses 60s timeout")
		require.Equal(t, defaultEndpoint, p.endpoint)
		require.Equal(t, defaultModel, p.model)
		require.Equal(t, "llama3.1", p.DefaultModel())
	})

	t.Run("explicit overrides win", func(t *testing.T) {
		custom := &http.Client{Timeout: 5 * time.Second}
		p := New(custom, "http://example:1234", "qwen2")
		require.Same(t, custom, p.http, "supplied client must be used verbatim")
		require.Equal(t, "http://example:1234", p.endpoint)
		require.Equal(t, "qwen2", p.DefaultModel())
	})
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	cases := []struct {
		name      string
		suffix    string // appended to the bare server URL
		wantStore string // expected stored endpoint suffix shape ("" = exact server URL)
	}{
		{name: "no slash", suffix: ""},
		{name: "single trailing slash", suffix: "/"},
		{name: "triple trailing slash", suffix: "///"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Server replies 200 to /api/tags so Available succeeds and we can
			// inspect the exact path that was hit.
			srv, read := newCapturingServer(t, http.StatusOK, "")
			p := New(srv.Client(), srv.URL+tc.suffix, "")
			require.False(t, strings.HasSuffix(p.endpoint, "/"),
				"stored endpoint must not retain a trailing slash")
			require.NoError(t, p.Available(context.Background()))
			method, path, _ := read()
			require.Equal(t, http.MethodGet, method)
			// The decisive regression check: exactly "/api/tags", no "//api/tags".
			require.Equal(t, "/api/tags", path)
		})
	}
}

func TestName(t *testing.T) {
	require.Equal(t, "ollama", New(nil, "", "").Name())
}

func TestAvailable_OK(t *testing.T) {
	srv, read := newCapturingServer(t, http.StatusOK, `{"models":[]}`)
	p := New(srv.Client(), srv.URL, "")
	require.NoError(t, p.Available(context.Background()))
	method, path, _ := read()
	require.Equal(t, http.MethodGet, method)
	require.Equal(t, "/api/tags", path)
}

func TestAvailable_Non200(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv, _ := newCapturingServer(t, code, "")
			p := New(srv.Client(), srv.URL, "")
			err := p.Available(context.Background())
			require.Error(t, err)
			require.Contains(t, err.Error(), "/api/tags")
			require.Contains(t, err.Error(), "http")
			require.Contains(t, err.Error(), strconv.Itoa(code))
		})
	}
}

func TestAvailable_Unreachable(t *testing.T) {
	// Start a server, capture its (now valid) URL, then close it so the dial
	// fails against a dead local port — no real network involved.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	client := srv.Client()
	srv.Close()

	p := New(client, url, "")
	err := p.Available(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "ollama unreachable at")
	require.Contains(t, err.Error(), url)
}

func TestEvaluate_RequestShape(t *testing.T) {
	const okBody = `{"message":{"role":"assistant","content":"hi"},"model":"m"}`

	t.Run("plain request omits format and options", func(t *testing.T) {
		srv, read := newCapturingServer(t, http.StatusOK, okBody)
		p := New(srv.Client(), srv.URL, "")
		_, err := p.Evaluate(context.Background(), llm.Request{System: "sys", User: "usr"})
		require.NoError(t, err)

		method, path, body := read()
		require.Equal(t, http.MethodPost, method)
		require.Equal(t, "/api/chat", path)
		require.Equal(t, false, body["stream"])
		require.NotContains(t, body, "format")
		require.NotContains(t, body, "options")

		msgs, ok := body["messages"].([]any)
		require.True(t, ok, "messages must be a JSON array")
		require.Len(t, msgs, 2)
		sys := msgs[0].(map[string]any)
		usr := msgs[1].(map[string]any)
		require.Equal(t, "system", sys["role"])
		require.Equal(t, "sys", sys["content"])
		require.Equal(t, "user", usr["role"])
		require.Equal(t, "usr", usr["content"])
	})

	t.Run("JSONMode sets format json", func(t *testing.T) {
		srv, read := newCapturingServer(t, http.StatusOK, okBody)
		p := New(srv.Client(), srv.URL, "")
		_, err := p.Evaluate(context.Background(), llm.Request{User: "x", JSONMode: true})
		require.NoError(t, err)
		_, _, body := read()
		require.Equal(t, "json", body["format"])
	})

	t.Run("MaxTokens>0 sets options.num_predict", func(t *testing.T) {
		srv, read := newCapturingServer(t, http.StatusOK, okBody)
		p := New(srv.Client(), srv.URL, "")
		_, err := p.Evaluate(context.Background(), llm.Request{User: "x", MaxTokens: 256})
		require.NoError(t, err)
		_, _, body := read()
		opts, ok := body["options"].(map[string]any)
		require.True(t, ok, "options must be present when MaxTokens>0")
		// JSON numbers decode to float64.
		require.Equal(t, float64(256), opts["num_predict"])
	})

	t.Run("MaxTokens==0 omits options entirely", func(t *testing.T) {
		srv, read := newCapturingServer(t, http.StatusOK, okBody)
		p := New(srv.Client(), srv.URL, "")
		_, err := p.Evaluate(context.Background(), llm.Request{User: "x", MaxTokens: 0})
		require.NoError(t, err)
		_, _, body := read()
		require.NotContains(t, body, "options")
	})
}

func TestEvaluate_ModelOverride(t *testing.T) {
	const okBody = `{"message":{"content":"ok"},"model":"m"}`
	cases := []struct {
		name      string
		provider  string // model passed to New
		reqModel  string // llm.Request.Model
		wantModel string // expected wire "model"
	}{
		{name: "empty req falls back to provider default", provider: "", reqModel: "", wantModel: defaultModel},
		{name: "empty req uses configured provider model", provider: "phi3", reqModel: "", wantModel: "phi3"},
		{name: "req model overrides provider", provider: "phi3", reqModel: "mistral", wantModel: "mistral"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, read := newCapturingServer(t, http.StatusOK, okBody)
			p := New(srv.Client(), srv.URL, tc.provider)
			_, err := p.Evaluate(context.Background(), llm.Request{User: "hi", Model: tc.reqModel})
			require.NoError(t, err)
			_, _, body := read()
			require.Equal(t, tc.wantModel, body["model"])
		})
	}
}

func TestEvaluate_ResponseMapping(t *testing.T) {
	resp := `{
		"message":{"role":"assistant","content":"the answer"},
		"model":"llama3.1:8b",
		"done_reason":"stop",
		"prompt_eval_count":11,
		"eval_count":42
	}`
	srv, _ := newCapturingServer(t, http.StatusOK, resp)
	p := New(srv.Client(), srv.URL, "")
	got, err := p.Evaluate(context.Background(), llm.Request{User: "q"})
	require.NoError(t, err)
	require.Equal(t, "the answer", got.Text)
	require.Equal(t, "llama3.1:8b", got.Model)
	require.Equal(t, "stop", got.Reason)
	require.Equal(t, 11, got.Usage.InputTokens)
	require.Equal(t, 42, got.Usage.OutputTokens)
}

func TestEvaluate_Non200IncludesBody(t *testing.T) {
	srv, _ := newCapturingServer(t, http.StatusInternalServerError, "model not found")
	p := New(srv.Client(), srv.URL, "")
	_, err := p.Evaluate(context.Background(), llm.Request{User: "q"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ollama http 500")
	require.Contains(t, err.Error(), "model not found")
}

func TestEvaluate_DecodeError(t *testing.T) {
	srv, _ := newCapturingServer(t, http.StatusOK, "{not json")
	p := New(srv.Client(), srv.URL, "")
	_, err := p.Evaluate(context.Background(), llm.Request{User: "q"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ollama decode")
}

func TestEvaluate_TransportError(t *testing.T) {
	// Closed server => dial to a dead local port; no real network.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	client := srv.Client()
	srv.Close()

	p := New(client, url, "")
	_, err := p.Evaluate(context.Background(), llm.Request{User: "q"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ollama call")
}

func TestEvaluate_ContextCancelled(t *testing.T) {
	// Handler blocks until the request context is done so the only way out is
	// ctx cancellation propagating into the in-flight HTTP request.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	p := New(srv.Client(), srv.URL, "")
	_, err := p.Evaluate(ctx, llm.Request{User: "q"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ollama call")
}
