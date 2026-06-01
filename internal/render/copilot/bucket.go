// Package copilot implements the GitHub Copilot aggregator-per-glob harness adapter.
//
// Copilot reads .github/instructions/<bucket>.instructions.md files. Each file
// has a YAML frontmatter `applyTo:` field that scopes when the instructions
// apply. Skills with the same sorted globs set bucket together; skills with
// activation=always bucket under a special "always" bucket with applyTo:"**".
// Skills with activation=agent or activation=manual are NOT renderable here.
package copilot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/pkg/adept"
)

// BucketKey identifies a Copilot bucket. Two values are reserved:
//   - "always" — for activation=always skills.
//   - "bucket-<sha8>" — for activation=globs skills, hashed over sorted globs.
type BucketKey string

// AlwaysKey is the bucket key for activation=always skills.
const AlwaysKey BucketKey = "always"

// BucketSpec describes one Copilot output file's framing.
type BucketSpec struct {
	// Key uniquely identifies the bucket within a project.
	Key BucketKey
	// Globs is the sorted, deduplicated set of globs this bucket applies to.
	// Empty for the AlwaysKey bucket (which maps applyTo to "**").
	Globs []string
	// ApplyTo is the value emitted in the Copilot frontmatter `applyTo:` field.
	// Either "**" or a comma-joined glob list.
	ApplyTo string
	// Path is the relative output path under the project root, e.g.
	// ".github/instructions/always.instructions.md".
	Path string
}

// Bucketer assigns a Copilot bucket to a canonical skill.
type Bucketer interface {
	// KeyFor returns the bucket spec a skill belongs to. The second return is
	// false when the skill is not eligible for Copilot (e.g. activation=agent).
	KeyFor(s *adept.Skill) (BucketSpec, bool)
}

type bucketer struct{}

// NewBucketer returns the default deterministic Bucketer.
func NewBucketer() Bucketer { return &bucketer{} }

const (
	// BucketDir is the relative directory holding bucket files.
	BucketDir = ".github/instructions"
	// FileSuffix is appended to the bucket key to form the filename.
	FileSuffix = ".instructions.md"
)

// KeyFor implements Bucketer.
//
//   - activation=always → AlwaysKey bucket with applyTo:"**".
//   - activation=globs (non-empty globs) → bucket-<sha8(sorted-globs)>.
//   - activation=agent / activation=manual → (zero, false).
//   - activation=globs but Globs is empty → (zero, false): defensive, schema
//     normally forbids it.
func (b *bucketer) KeyFor(s *adept.Skill) (BucketSpec, bool) {
	if s == nil {
		return BucketSpec{}, false
	}
	switch s.Activation {
	case adept.ActivationAlways:
		return BucketSpec{
			Key:     AlwaysKey,
			Globs:   nil,
			ApplyTo: "**",
			Path:    bucketPath(AlwaysKey),
		}, true
	case adept.ActivationGlobs:
		gs := normalizeGlobs(s.Globs)
		if len(gs) == 0 {
			return BucketSpec{}, false
		}
		key := bucketKeyForGlobs(gs)
		return BucketSpec{
			Key:     key,
			Globs:   gs,
			ApplyTo: strings.Join(gs, ","),
			Path:    bucketPath(key),
		}, true
	default:
		// agent, manual, or anything unrecognized: not Copilot-eligible.
		return BucketSpec{}, false
	}
}

// normalizeGlobs returns a sorted, deduplicated copy of the input.
func normalizeGlobs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, g := range in {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if _, ok := seen[g]; ok {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// bucketKeyForGlobs hashes the sorted globs with sha256 and takes the first
// 8 hex chars to form "bucket-<sha8>".
func bucketKeyForGlobs(sorted []string) BucketKey {
	h := sha256.New()
	for i, g := range sorted {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(g))
	}
	sum := h.Sum(nil)
	return BucketKey(fmt.Sprintf("bucket-%s", hex.EncodeToString(sum)[:8]))
}

// bucketPath returns the relative on-disk path for a bucket file.
func bucketPath(key BucketKey) string {
	return fmt.Sprintf("%s/%s%s", BucketDir, string(key), FileSuffix)
}
