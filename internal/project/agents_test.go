package project

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

func sampleAgent(id string) *adept.Agent {
	return &adept.Agent{
		ID:          id,
		Description: "desc " + id,
		Mode:        adept.AgentModeSubagent,
		Body:        "You are " + id + ".\n",
	}
}

func TestProject_Agents_EmptyState(t *testing.T) {
	p, _ := newProject(t)
	require.False(t, p.HasAgent("reviewer"))
	list, err := p.ListAgents()
	require.NoError(t, err)
	require.Empty(t, list)
	_, err = p.GetAgent("reviewer")
	require.ErrorIs(t, err, adept.ErrAgentNotFound)
}

func TestProject_Agents_InstallGetListRemove(t *testing.T) {
	p, root := newProject(t)
	require.NoError(t, p.InstallAgent(sampleAgent("reviewer")))
	require.NoError(t, p.InstallAgent(sampleAgent("fixer")))

	require.FileExists(t, filepath.Join(root, adept.BaseDirName, adept.AgentsDirName, "reviewer.md"))
	require.True(t, p.HasAgent("reviewer"))

	got, err := p.GetAgent("reviewer")
	require.NoError(t, err)
	require.Equal(t, "reviewer", got.ID)
	require.Equal(t, "desc reviewer", got.Description)
	require.Equal(t, "You are reviewer.\n", got.Body)

	list, err := p.ListAgents()
	require.NoError(t, err)
	require.Len(t, list, 2)
	// Sorted by id.
	require.Equal(t, "fixer", list[0].ID)
	require.Equal(t, "reviewer", list[1].ID)

	require.NoError(t, p.UninstallAgent("fixer"))
	require.False(t, p.HasAgent("fixer"))
	require.ErrorIs(t, p.UninstallAgent("fixer"), adept.ErrAgentNotFound)
}

func TestProject_Agents_IDGuards(t *testing.T) {
	p, _ := newProject(t)
	require.ErrorIs(t, p.InstallAgent(nil), adept.ErrAgentInvalid)
	require.ErrorIs(t, p.InstallAgent(&adept.Agent{ID: "../evil", Description: "d"}), adept.ErrAgentInvalid)
	require.ErrorIs(t, p.UninstallAgent("../evil"), adept.ErrAgentInvalid)
	_, err := p.GetAgent("../evil")
	require.ErrorIs(t, err, adept.ErrAgentInvalid)
	require.False(t, p.HasAgent("../evil"))
}

func TestProject_Agents_FilenameAuthoritative(t *testing.T) {
	p, _ := newProject(t)
	a := sampleAgent("canonical-name")
	require.NoError(t, p.InstallAgent(a))
	got, err := p.GetAgent("canonical-name")
	require.NoError(t, err)
	require.Equal(t, "canonical-name", got.ID)
}
