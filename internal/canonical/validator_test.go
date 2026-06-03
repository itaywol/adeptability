package canonical

import (
	"strings"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func newTestValidator(t *testing.T) Validator {
	t.Helper()
	v, err := NewValidator()
	require.NoError(t, err)
	return v
}

func TestValidator_Valid(t *testing.T) {
	t.Parallel()
	v := newTestValidator(t)
	s := &adept.Skill{
		ID:          "ok-skill",
		Description: "fine",
		Activation:  adept.ActivationAgent,
	}
	require.NoError(t, v.Validate(s))
}

func TestValidator_HarnessOverrideValid(t *testing.T) {
	t.Parallel()
	v := newTestValidator(t)
	s := &adept.Skill{
		ID:          "ok-skill",
		Description: "fine",
		Activation:  adept.ActivationAgent,
		Model:       "claude-opus-4-8",
		Harness: map[string]map[string]any{
			"claude-code": {"effort": "high", "allowed-tools": []any{"Bash"}},
		},
	}
	require.NoError(t, v.Validate(s))
}

func TestValidator_HarnessOverrideCannotHijackIdentity(t *testing.T) {
	t.Parallel()
	v := newTestValidator(t)
	for _, key := range []string{"id", "name", "description"} {
		s := &adept.Skill{
			ID:          "ok-skill",
			Description: "fine",
			Activation:  adept.ActivationAgent,
			Harness:     map[string]map[string]any{"claude-code": {key: "hijack"}},
		}
		err := v.Validate(s)
		require.Error(t, err, "override of identity field %q must be rejected", key)
		require.ErrorIs(t, err, adept.ErrSkillInvalid)
	}
}

func TestValidator_NilSkill(t *testing.T) {
	t.Parallel()
	v := newTestValidator(t)
	err := v.Validate(nil)
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestValidator_InvalidID(t *testing.T) {
	t.Parallel()
	v := newTestValidator(t)
	s := &adept.Skill{
		ID:          "Bad-ID",
		Description: "x",
	}
	err := v.Validate(s)
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestValidator_MissingDescription(t *testing.T) {
	t.Parallel()
	v := newTestValidator(t)
	s := &adept.Skill{
		ID: "good-id",
	}
	err := v.Validate(s)
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestValidator_GlobsActivationRequiresGlobs(t *testing.T) {
	t.Parallel()
	v := newTestValidator(t)
	s := &adept.Skill{
		ID:          "ok-skill",
		Description: "x",
		Activation:  adept.ActivationGlobs,
	}
	err := v.Validate(s)
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)

	s.Globs = []string{"**/*.go"}
	require.NoError(t, v.Validate(s))
}

func TestValidator_DescriptionTooLong(t *testing.T) {
	t.Parallel()
	v := newTestValidator(t)
	s := &adept.Skill{
		ID:          "ok-skill",
		Description: strings.Repeat("x", 281),
	}
	err := v.Validate(s)
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}
