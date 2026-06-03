package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/itaywol/adeptability/internal/llm"
	"github.com/stretchr/testify/require"
)

// Regression: a non-200 response from a (user-configurable) endpoint must
// not be read unbounded into the error string. The read is capped via
// io.LimitReader(res.Body, 8<<10).
func TestEvaluate_BoundsErrorBody_Regress(t *testing.T) {
	const limit = 8 << 10
	big := strings.Repeat("E", 5*1024*1024) // 5 MiB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	p := New(srv.Client(), srv.URL, "")
	_, err := p.Evaluate(context.Background(), llm.Request{User: "hi"})
	require.Error(t, err)
	msg := err.Error()
	bodyPart := msg[strings.Index(msg, ":")+1:]
	require.LessOrEqual(t, len(strings.TrimSpace(bodyPart)), limit,
		"error body must be bounded by io.LimitReader")
	require.Contains(t, msg, "500")
}
