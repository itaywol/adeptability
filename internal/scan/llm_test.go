package scan

import (
	"context"
	"errors"
	"testing"

	"github.com/itaywol/adeptability/internal/llm"
	"github.com/stretchr/testify/require"
)

type stubProvider struct {
	text string
	err  error
}

func (s *stubProvider) Name() string                    { return "stub" }
func (s *stubProvider) DefaultModel() string            { return "stub-1" }
func (s *stubProvider) Available(context.Context) error { return nil }
func (s *stubProvider) Evaluate(context.Context, llm.Request) (llm.Response, error) {
	if s.err != nil {
		return llm.Response{}, s.err
	}
	return llm.Response{Text: s.text}, nil
}

func TestLLMReviewer_NilProvider_PassesThrough(t *testing.T) {
	r := &LLMReviewer{}
	rep, err := r.Review(context.Background(), Target{Name: "x"}, Report{Target: "x"})
	require.NoError(t, err)
	require.Equal(t, "x", rep.Target)
}

func TestLLMReviewer_DropsFalsePositive(t *testing.T) {
	static := Report{
		Target: "demo",
		Findings: []Finding{
			{ID: "SKILL-SEC-003", Category: CategoryMaliciousCode, Severity: SeverityHigh, Issue: "sudo"},
			{ID: "SKILL-SEC-201", Category: CategoryPromptInjection, Severity: SeverityHigh, Issue: "ignore previous"},
		},
	}
	prov := &stubProvider{text: `{
		"verdicts": [
			{"id":"SKILL-SEC-003","confirmed":false,"reason":"sudo only in code fence"},
			{"id":"SKILL-SEC-201","confirmed":true,"reason":"clear injection phrasing"}
		],
		"additional": [],
		"overallRisk": "high",
		"summary": "ok"
	}`}
	r := &LLMReviewer{Provider: prov}
	out, err := r.Review(context.Background(), Target{Name: "demo", Body: []byte("...")}, static)
	require.NoError(t, err)
	require.Len(t, out.Findings, 1)
	require.Equal(t, "SKILL-SEC-201", out.Findings[0].ID)
}

func TestLLMReviewer_AppendsAdditional(t *testing.T) {
	static := Report{Target: "demo"}
	prov := &stubProvider{text: `{
		"verdicts": [],
		"additional": [
			{
				"category":"prompt-injection",
				"severity":"high",
				"confidence":"medium",
				"issue":"polite jailbreak",
				"evidence":"please ignore...",
				"risk":"override safety",
				"remediation":"remove the phrasing"
			}
		],
		"overallRisk":"high",
		"summary":"x"
	}`}
	r := &LLMReviewer{Provider: prov}
	out, err := r.Review(context.Background(), Target{Name: "demo", Body: []byte("...")}, static)
	require.NoError(t, err)
	require.Len(t, out.Findings, 1)
	require.Equal(t, "SKILL-LLM-001", out.Findings[0].ID)
	require.Equal(t, SeverityHigh, out.Findings[0].Severity)
}

func TestLLMReviewer_ProviderErrorReturnsStatic(t *testing.T) {
	static := Report{Target: "demo", Findings: []Finding{{ID: "X", Severity: SeverityLow}}}
	r := &LLMReviewer{Provider: &stubProvider{err: errors.New("network")}}
	out, err := r.Review(context.Background(), Target{Name: "demo"}, static)
	require.Error(t, err)
	require.Len(t, out.Findings, 1, "static report still surfaces on provider error")
}

func TestLLMReviewer_ParsesFencedJSON(t *testing.T) {
	static := Report{}
	prov := &stubProvider{text: "```json\n{\"verdicts\":[],\"additional\":[],\"overallRisk\":\"clean\",\"summary\":\"ok\"}\n```"}
	r := &LLMReviewer{Provider: prov}
	_, err := r.Review(context.Background(), Target{Name: "x"}, static)
	require.NoError(t, err)
}
