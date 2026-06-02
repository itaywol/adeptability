// Package ollama is the local Ollama implementation of llm.Provider.
// Default endpoint is the conventional 127.0.0.1:11434; users running a
// remote Ollama set it via `adept config llm set ollama --endpoint`.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/itaywol/adeptability/internal/llm"
)

const (
	defaultEndpoint = "http://127.0.0.1:11434"
	defaultModel    = "llama3.1"
)

// Provider implements llm.Provider against a local or self-hosted
// Ollama server. No auth (Ollama doesn't gate by default).
type Provider struct {
	http     *http.Client
	endpoint string
	model    string
}

// New builds a Provider. Empty endpoint/model fall back to the local
// 127.0.0.1:11434 and llama3.1.
func New(hc *http.Client, endpoint, model string) *Provider {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if model == "" {
		model = defaultModel
	}
	return &Provider{http: hc, endpoint: strings.TrimRight(endpoint, "/"), model: model}
}

func (p *Provider) Name() string         { return "ollama" }
func (p *Provider) DefaultModel() string { return p.model }

// Available pings /api/tags. A fast probe — fails fast when the local
// server is not running, which is the common-case "i forgot to launch
// ollama" path.
func (p *Provider) Available(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint+"/api/tags", nil)
	if err != nil {
		return err
	}
	res, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("ollama unreachable at %s: %w", p.endpoint, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama %s/api/tags: http %d", p.endpoint, res.StatusCode)
	}
	return nil
}

// Evaluate POSTs to /api/chat with stream=false. JSONMode flips the
// `format:"json"` flag which Ollama honors as a hard JSON constraint
// on the model output.
func (p *Provider) Evaluate(ctx context.Context, req llm.Request) (llm.Response, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	payload := map[string]any{
		"model":  model,
		"stream": false,
		"messages": []map[string]any{
			{"role": "system", "content": req.System},
			{"role": "user", "content": req.User},
		},
	}
	if req.JSONMode {
		payload["format"] = "json"
	}
	if req.MaxTokens > 0 {
		payload["options"] = map[string]any{"num_predict": req.MaxTokens}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return llm.Response{}, fmt.Errorf("ollama marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return llm.Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := p.http.Do(httpReq)
	if err != nil {
		return llm.Response{}, fmt.Errorf("ollama call: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(res.Body)
		return llm.Response{}, fmt.Errorf("ollama http %d: %s", res.StatusCode, string(errBody))
	}
	var decoded struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		Model              string `json:"model"`
		DoneReason         string `json:"done_reason"`
		PromptEvalCount    int    `json:"prompt_eval_count"`
		EvalCount          int    `json:"eval_count"`
	}
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return llm.Response{}, fmt.Errorf("ollama decode: %w", err)
	}
	return llm.Response{
		Text:   decoded.Message.Content,
		Model:  decoded.Model,
		Reason: decoded.DoneReason,
		Usage: llm.Usage{
			InputTokens:  decoded.PromptEvalCount,
			OutputTokens: decoded.EvalCount,
		},
	}, nil
}
