// Package anthropic is the Anthropic Messages-API implementation of
// llm.Provider. Reads ANTHROPIC_API_KEY at call time; never stores it.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/itaywol/adeptability/internal/llm"
)

// Default endpoint + version. Override the endpoint via the LLMConfig
// `endpoint` field for Anthropic-compatible proxies.
const (
	defaultEndpoint     = "https://api.anthropic.com/v1/messages"
	defaultAPIVersion   = "2023-06-01"
	defaultModel        = "claude-haiku-4-5-20251001"
	envAPIKey           = "ANTHROPIC_API_KEY"
)

// Provider implements llm.Provider against Anthropic's Messages API.
type Provider struct {
	http     *http.Client
	endpoint string
	model    string
}

// New constructs a Provider. Pass "" for endpoint/model to use the
// safer Sonnet 4.5 default.
func New(hc *http.Client, endpoint, model string) *Provider {
	if hc == nil {
		hc = http.DefaultClient
	}
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if model == "" {
		model = defaultModel
	}
	return &Provider{http: hc, endpoint: endpoint, model: model}
}

func (p *Provider) Name() string         { return "anthropic" }
func (p *Provider) DefaultModel() string { return p.model }

func (p *Provider) Available(ctx context.Context) error {
	if os.Getenv(envAPIKey) == "" {
		return fmt.Errorf("anthropic: %s not set in environment", envAPIKey)
	}
	return nil
}

// Evaluate POSTs to /v1/messages with the canonical anthropic-version
// header and Bearer auth. JSONMode flips the `system` prompt prefix so
// the model is steered to emit raw JSON; the wire format remains text.
func (p *Provider) Evaluate(ctx context.Context, req llm.Request) (llm.Response, error) {
	key := os.Getenv(envAPIKey)
	if key == "" {
		return llm.Response{}, fmt.Errorf("anthropic: %s not set", envAPIKey)
	}
	model := req.Model
	if model == "" {
		model = p.model
	}
	maxTok := req.MaxTokens
	if maxTok == 0 {
		maxTok = 1024
	}
	system := req.System
	if req.JSONMode {
		system = strings.TrimSpace(system + "\n\nRespond with valid JSON only. No prose, no markdown fences.")
	}
	payload := map[string]any{
		"model":      model,
		"max_tokens": maxTok,
		"system":     system,
		"messages": []map[string]any{
			{"role": "user", "content": req.User},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return llm.Response{}, fmt.Errorf("anthropic marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return llm.Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", key)
	httpReq.Header.Set("anthropic-version", defaultAPIVersion)
	res, err := p.http.Do(httpReq)
	if err != nil {
		return llm.Response{}, fmt.Errorf("anthropic call: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(res.Body)
		return llm.Response{}, fmt.Errorf("anthropic http %d: %s", res.StatusCode, string(errBody))
	}
	var decoded struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Model      string `json:"model"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return llm.Response{}, fmt.Errorf("anthropic decode: %w", err)
	}
	var text strings.Builder
	for _, c := range decoded.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return llm.Response{
		Text:   text.String(),
		Model:  decoded.Model,
		Reason: decoded.StopReason,
		Usage: llm.Usage{
			InputTokens:  decoded.Usage.InputTokens,
			OutputTokens: decoded.Usage.OutputTokens,
		},
	}, nil
}
