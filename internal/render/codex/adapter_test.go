package codex

import (
	"context"
	"io/fs"
	"sync"
	"testing"

	"github.com/itaywol/adeptability/internal/budget"
	"github.com/itaywol/adeptability/pkg/adept"
)

// fakeReader is a minimal in-memory FileReader used by adapter tests.
type fakeReader struct {
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]struct{}
}

func newFakeReader() *fakeReader {
	return &fakeReader{files: map[string][]byte{}, dirs: map[string]struct{}{}}
}

func (w *fakeReader) ReadFile(path string) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	b, ok := w.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

func (w *fakeReader) Exists(path string) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.files[path]; ok {
		return true, nil
	}
	if _, ok := w.dirs[path]; ok {
		return true, nil
	}
	return false, nil
}

func (w *fakeReader) mkdir(p string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dirs[p] = struct{}{}
}

func TestDetect_AgentsFile(t *testing.T) {
	w := newFakeReader()
	w.files["/proj/AGENTS.md"] = []byte("x")
	a := NewAdapterWithReader(New(), budget.NewPacker(), nil, w)
	ok, err := a.Detect("/proj")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !ok {
		t.Fatalf("expected detect=true with AGENTS.md present")
	}
}

func TestDetect_CodexDir(t *testing.T) {
	w := newFakeReader()
	w.mkdir("/proj/.codex")
	a := NewAdapterWithReader(New(), budget.NewPacker(), nil, w)
	ok, err := a.Detect("/proj")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !ok {
		t.Fatalf("expected detect=true with .codex/ present")
	}
}

func TestDetect_None(t *testing.T) {
	w := newFakeReader()
	a := NewAdapterWithReader(New(), budget.NewPacker(), nil, w)
	ok, err := a.Detect("/proj")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if ok {
		t.Fatalf("expected detect=false with no markers")
	}
}

func TestDetect_EmptyRoot(t *testing.T) {
	a := NewAdapterWithReader(New(), budget.NewPacker(), nil, newFakeReader())
	if _, err := a.Detect(""); err == nil {
		t.Fatalf("expected error for empty root")
	}
}

func TestValidate_Synced(t *testing.T) {
	w := newFakeReader()
	r := New()
	a := NewAdapterWithReader(r, budget.NewPacker(), nil, w)
	parts := renderAll(t, []*adept.Skill{{ID: "x", Description: "X", Body: "body\n"}})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	// Materialize the expected bytes at the on-disk path.
	w.files["/proj/AGENTS.md"] = outs[0].Bytes
	rep, err := a.Validate("/proj", outs)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Synced) != 1 || rep.Synced[0] != "codex" {
		t.Fatalf("expected Synced=[codex], got %+v", rep)
	}
}

func TestValidate_Missing(t *testing.T) {
	w := newFakeReader()
	r := New()
	a := NewAdapterWithReader(r, budget.NewPacker(), nil, w)
	parts := renderAll(t, []*adept.Skill{{ID: "x", Description: "X", Body: "body\n"}})
	outs, _ := a.Aggregate(context.Background(), parts, 0)
	rep, err := a.Validate("/proj", outs)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Missing) != 1 || rep.Missing[0] != "codex" {
		t.Fatalf("expected Missing=[codex], got %+v", rep)
	}
}

func TestValidate_Drifted(t *testing.T) {
	w := newFakeReader()
	r := New()
	a := NewAdapterWithReader(r, budget.NewPacker(), nil, w)
	parts := renderAll(t, []*adept.Skill{{ID: "x", Description: "X", Body: "body\n"}})
	outs, _ := a.Aggregate(context.Background(), parts, 0)
	w.files["/proj/AGENTS.md"] = []byte("something else entirely")
	rep, err := a.Validate("/proj", outs)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Drifted) != 1 || rep.Drifted[0] != "codex" {
		t.Fatalf("expected Drifted=[codex], got %+v", rep)
	}
}

func TestValidate_NoExpected(t *testing.T) {
	a := NewAdapterWithReader(New(), budget.NewPacker(), nil, newFakeReader())
	rep, err := a.Validate("/proj", nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Synced) != 1 || rep.Synced[0] != "codex" {
		t.Fatalf("expected vacuously Synced, got %+v", rep)
	}
}
