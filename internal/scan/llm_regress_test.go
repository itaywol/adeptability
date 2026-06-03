package scan

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression: a critical overallRisk verdict with no itemized
// `additional` finding must still raise the report's Worst() (and thus
// the install gate) via a synthetic SKILL-LLM-OVERALL finding.
func TestLLMRegress_OverallRiskCriticalInjected(t *testing.T) {
	static := Report{Target: "demo"}
	prov := &stubProvider{text: `{
		"verdicts": [],
		"additional": [],
		"overallRisk": "critical",
		"summary": "coordinated multi-step exfiltration"
	}`}
	r := &LLMReviewer{Provider: prov}
	out, err := r.Review(context.Background(), Target{Name: "demo", Body: []byte("...")}, static)
	require.NoError(t, err)
	require.Equal(t, SeverityCritical, out.Worst(), "overallRisk critical must raise Worst()")
	require.True(t, containsID(out, "SKILL-LLM-OVERALL"))
	// The summary becomes the issue text.
	var issue string
	for _, f := range out.Findings {
		if f.ID == "SKILL-LLM-OVERALL" {
			issue = f.Issue
		}
	}
	require.Equal(t, "coordinated multi-step exfiltration", issue)
}

// When overallRisk does NOT exceed the per-finding severities, no
// synthetic finding is injected (avoid double-counting).
func TestLLMRegress_OverallRiskNotInjectedWhenNotHigher(t *testing.T) {
	static := Report{
		Target:   "demo",
		Findings: []Finding{{ID: "SKILL-SEC-003", Severity: SeverityHigh}},
	}
	prov := &stubProvider{text: `{
		"verdicts": [{"id":"SKILL-SEC-003","confirmed":true,"reason":"real"}],
		"additional": [],
		"overallRisk": "high",
		"summary": "ok"
	}`}
	r := &LLMReviewer{Provider: prov}
	out, err := r.Review(context.Background(), Target{Name: "demo", Body: []byte("...")}, static)
	require.NoError(t, err)
	require.False(t, containsID(out, "SKILL-LLM-OVERALL"))
	require.Len(t, out.Findings, 1)
}

// An unrecognized overallRisk value must be ignored (not injected),
// matching the validate-first contract.
func TestLLMRegress_OverallRiskUnknownIgnored(t *testing.T) {
	static := Report{Target: "demo"}
	prov := &stubProvider{text: `{
		"verdicts": [],
		"additional": [],
		"overallRisk": "catastrophic",
		"summary": "x"
	}`}
	r := &LLMReviewer{Provider: prov}
	out, err := r.Review(context.Background(), Target{Name: "demo", Body: []byte("...")}, static)
	require.NoError(t, err)
	require.False(t, containsID(out, "SKILL-LLM-OVERALL"))
	require.Equal(t, SeverityClean, out.Worst())
}
