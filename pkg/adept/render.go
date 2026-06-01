package adept

import (
	"context"
	"io/fs"
)

// RenderInput is what a Renderer receives to produce harness-ready bytes.
type RenderInput struct {
	Skill   *Skill
	Harness HarnessSpec
	Project ProjectInfo
}

// SideFile is a sidecar emitted alongside the main rendered file.
type SideFile struct {
	RelPath string
	Bytes   []byte
	Mode    fs.FileMode
}

// RenderOutput is the result of rendering one skill for one harness.
// Path is relative to the project root.
type RenderOutput struct {
	Path     string
	Bytes    []byte
	Mode     fs.FileMode
	Sidecars []SideFile

	// SkillID is set by the renderer so aggregators can group/sort.
	SkillID string
	// SkillVersion lets aggregators prioritize newer skills under budget pressure.
	SkillVersion int
	// Warnings surface non-fatal rendering decisions (e.g. sidecars dropped
	// because the target harness is single-file).
	Warnings []string
}

// Renderer translates a single canonical Skill into harness-specific bytes.
type Renderer interface {
	Render(ctx context.Context, in RenderInput) (RenderOutput, error)
}
