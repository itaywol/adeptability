package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/itaywol/adeptability/internal/llm"
	"github.com/stretchr/testify/require"
)

// TestNew_Defaults pins each defaulting branch in New independently: a nil
// http.Client falls back to http.DefaultClient, empty endpoint/model fall
// back to the package defaults, and non-empty overrides are preserved.
func TestNew_Defaults(t *testing.T) {
	custom := &http.Client{}
	cases := []struct {
		name         string
		hc           *http.Client
		endpoint     string
		model        string
		wantEndpoint string
		wantModel    string
		wantDefaultC bool // http should be http.DefaultClient
	}{
		{
			name:         "all empty -> defaults",
			hc:           nil,
			endpoint:     "",
			model:        "",
			wantEndpoint: defaultEndpoint,
			wantModel:    defaultModel,
			wantDefaultC: true,
		},
		{
			name:         "overrides preserved",
			hc:           custom,
			endpoint:     "https://proxy.example/x",
			model:        "my-model",
			wantEndpoint: "https://proxy.example/x",
			wantModel:    "my-model",
			wantDefaultC: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(tc.hc, tc.endpoint, tc.model)
			require.NotNil(t, p)
			require.NotNil(t, p.http, "http client must never be nil")
			require.Equal(t, tc.wantEndpoint, p.endpoint)
			require.Equal(t, tc.wantModel, p.model)
			if tc.wantDefaultC {
				require.Same(t, http.DefaultClient, p.http)
			} else {
				require.Same(t, custom, p.http)
			}
		})
	}
}

// TestProvider_NameAndDefaultModel pins the actual identifier and the actual
// default model. The doc comment on New claims a "Sonnet 4.5 default" but the
// real default is the Haiku constant; this locks down the real behavior so a
// silent constant change is caught.
func TestProvider_NameAndDefaultModel(t *testing.T) {
	def := New(nil, "", "")
	require.Equal(t, "anthropic", def.Name())
	require.Equal(t, "claude-haiku-4-5-20251001", def.DefaultModel())
	require.Equal(t, defaultModel, def.DefaultModel())

	override := New(nil, "", "claude-opus-x")
	require.Equal(t, "claude-opus-x", override.DefaultModel())
}

// TestProvider_Available checks the env-var guard both ways. Mutates process
// env via t.Setenv, so it must not run in parallel.
func TestProvider_Available(t *testing.T) {
	p := New(nil, "", "")

	t.Run("unset returns error", func(t *testing.T) {
		t.Setenv(envAPIKey, "")
		err := p.Available(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), envAPIKey)
	})

	t.Run("set returns nil", func(t *testing.T) {
		t.Setenv(envAPIKey, "sk-test")
		require.NoError(t, p.Available(context.Background()))
	})
}

// TestEvaluate_MissingKeyReturnsError verifies the guard returns an error and,
// crucially, makes no HTTP attempt: the endpoint points at a server that fails
// the test if it is ever hit.
func TestEvaluate_MissingKeyReturnsError(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv(envAPIKey, "")
	p := New(srv.Client(), srv.URL, "")
	resp, err := p.Evaluate(context.Background(), llm.Request{User: "hi"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ANTHROPIC_API_KEY not set")
	require.Equal(t, llm.Response{}, resp)
	require.False(t, hit, "Evaluate must not contact the endpoint when key is missing")
}

// captured holds what the fake backend saw on the wire.
type captured struct {
	headers http.Header
	body    map[string]any
}

// newCaptureServer returns an httptest server that records the inbound request
// and replies with the given status/body. The capture pointer is populated on
// each request.
func newCaptureServer(t *testing.T, status int, respBody string) (*httptest.Server, *captured) {
	t.Helper()
	cap := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.headers = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &cap.body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

// goodResponse is a minimal well-formed Messages-API reply.
const goodResponse = `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","model":"claude-x","usage":{"input_tokens":1,"output_tokens":2}}`

// TestEvaluate_BuildsRequest asserts request-body construction: model/max_tokens
// defaulting vs honoring, user content placement, and required headers.
func TestEvaluate_BuildsRequest(t *testing.T) {
	cases := []struct {
		name        string
		req         llm.Request
		wantModel   string
		wantMaxTok  float64 // JSON numbers decode to float64
		wantContent string
	}{
		{
			name:        "defaults applied",
			req:         llm.Request{User: "hello"},
			wantModel:   defaultModel,
			wantMaxTok:  1024,
			wantContent: "hello",
		},
		{
			name:        "overrides honored",
			req:         llm.Request{User: "yo", Model: "claude-custom", MaxTokens: 77},
			wantModel:   "claude-custom",
			wantMaxTok:  77,
			wantContent: "yo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, cap := newCaptureServer(t, http.StatusOK, goodResponse)
			t.Setenv(envAPIKey, "sk-secret")
			p := New(srv.Client(), srv.URL, "")
			_, err := p.Evaluate(context.Background(), tc.req)
			require.NoError(t, err)

			require.Equal(t, tc.wantModel, cap.body["model"])
			require.Equal(t, tc.wantMaxTok, cap.body["max_tokens"])

			msgs, ok := cap.body["messages"].([]any)
			require.True(t, ok, "messages must be an array")
			require.Len(t, msgs, 1)
			first, ok := msgs[0].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "user", first["role"])
			require.Equal(t, tc.wantContent, first["content"])

			require.Equal(t, "application/json", cap.headers.Get("Content-Type"))
			require.Equal(t, "sk-secret", cap.headers.Get("x-api-key"))
			require.Equal(t, defaultAPIVersion, cap.headers.Get("anthropic-version"))
			// Code uses x-api-key, NOT Bearer auth (doc comment mismatch).
			require.Empty(t, cap.headers.Get("Authorization"))
		})
	}
}

// TestEvaluate_JSONModeAugmentsSystem verifies the system-prompt mutation:
// JSONMode appends the JSON directive (trimmed), JSONMode=false leaves it
// unchanged, and an empty System+JSONMode trims the leading whitespace.
func TestEvaluate_JSONModeAugmentsSystem(t *testing.T) {
	const directive = "Respond with valid JSON only. No prose, no markdown fences."
	cases := []struct {
		name       string
		req        llm.Request
		wantSystem string
	}{
		{
			name:       "jsonmode appends directive",
			req:        llm.Request{User: "u", System: "base rules", JSONMode: true},
			wantSystem: "base rules\n\n" + directive,
		},
		{
			name:       "jsonmode false passes system unchanged",
			req:        llm.Request{User: "u", System: "base rules", JSONMode: false},
			wantSystem: "base rules",
		},
		{
			name:       "empty system + jsonmode trims leading whitespace",
			req:        llm.Request{User: "u", System: "", JSONMode: true},
			wantSystem: directive,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, cap := newCaptureServer(t, http.StatusOK, goodResponse)
			t.Setenv(envAPIKey, "sk-secret")
			p := New(srv.Client(), srv.URL, "")
			_, err := p.Evaluate(context.Background(), tc.req)
			require.NoError(t, err)
			require.Equal(t, tc.wantSystem, cap.body["system"])
		})
	}
}

// TestEvaluate_HTTPErrorStatus verifies non-200 responses surface
// "anthropic http <code>: <body>" with the body echoed.
func TestEvaluate_HTTPErrorStatus(t *testing.T) {
	cases := []struct {
		name string
		code int
		body string
	}{
		{name: "bad request", code: http.StatusBadRequest, body: `{"error":"bad"}`},
		{name: "unauthorized", code: http.StatusUnauthorized, body: `{"error":"no auth"}`},
		{name: "rate limited", code: http.StatusTooManyRequests, body: `{"error":"rate limited"}`},
		{name: "server error", code: http.StatusInternalServerError, body: `boom`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newCaptureServer(t, tc.code, tc.body)
			t.Setenv(envAPIKey, "sk-secret")
			p := New(srv.Client(), srv.URL, "")
			resp, err := p.Evaluate(context.Background(), llm.Request{User: "hi"})
			require.Error(t, err)
			require.Equal(t, llm.Response{}, resp)
			require.Contains(t, err.Error(), "anthropic http")
			require.Contains(t, err.Error(), tc.body)
		})
	}
}

// TestEvaluate_DecodeError verifies a 200 with a non-JSON body surfaces an
// "anthropic decode" error rather than a silent zero Response.
func TestEvaluate_DecodeError(t *testing.T) {
	srv, _ := newCaptureServer(t, http.StatusOK, "not json at all")
	t.Setenv(envAPIKey, "sk-secret")
	p := New(srv.Client(), srv.URL, "")
	resp, err := p.Evaluate(context.Background(), llm.Request{User: "hi"})
	require.Error(t, err)
	require.Equal(t, llm.Response{}, resp)
	require.Contains(t, err.Error(), "anthropic decode")
}

// TestEvaluate_TransportError verifies that when the transport fails (server
// closed before the call), the error is wrapped as "anthropic call". No real
// network: the httptest server is closed first so Do fails immediately.
func TestEvaluate_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	client := srv.Client()
	url := srv.URL
	srv.Close() // close before use -> connection refused

	t.Setenv(envAPIKey, "sk-secret")
	p := New(client, url, "")
	resp, err := p.Evaluate(context.Background(), llm.Request{User: "hi"})
	require.Error(t, err)
	require.Equal(t, llm.Response{}, resp)
	require.Contains(t, err.Error(), "anthropic call")
}

// TestEvaluate_ResponseAggregation verifies the decoded payload is mapped
// correctly: text blocks are concatenated in order, non-text blocks skipped,
// and Model/Reason/Usage are carried through. Empty content -> empty Text.
func TestEvaluate_ResponseAggregation(t *testing.T) {
	cases := []struct {
		name       string
		respBody   string
		wantText   string
		wantModel  string
		wantReason string
		wantUsage  llm.Usage
	}{
		{
			name:       "concatenates text, skips tool_use",
			respBody:   `{"content":[{"type":"text","text":"foo"},{"type":"tool_use","text":"IGNORED"},{"type":"text","text":"bar"}],"stop_reason":"end_turn","model":"claude-x","usage":{"input_tokens":11,"output_tokens":22}}`,
			wantText:   "foobar",
			wantModel:  "claude-x",
			wantReason: "end_turn",
			wantUsage:  llm.Usage{InputTokens: 11, OutputTokens: 22},
		},
		{
			name:       "empty content yields empty text",
			respBody:   `{"content":[],"stop_reason":"max_tokens","model":"claude-y","usage":{"input_tokens":3,"output_tokens":0}}`,
			wantText:   "",
			wantModel:  "claude-y",
			wantReason: "max_tokens",
			wantUsage:  llm.Usage{InputTokens: 3, OutputTokens: 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newCaptureServer(t, http.StatusOK, tc.respBody)
			t.Setenv(envAPIKey, "sk-secret")
			p := New(srv.Client(), srv.URL, "")
			resp, err := p.Evaluate(context.Background(), llm.Request{User: "hi"})
			require.NoError(t, err)
			require.Equal(t, tc.wantText, resp.Text)
			require.Equal(t, tc.wantModel, resp.Model)
			require.Equal(t, tc.wantReason, resp.Reason)
			require.Equal(t, tc.wantUsage, resp.Usage)
		})
	}
}
