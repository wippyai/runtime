// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

type quitTestModel struct{}

func (quitTestModel) Init() tea.Cmd {
	return tea.Quit
}

func (quitTestModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	return quitTestModel{}, nil
}

func (quitTestModel) View() string {
	return ""
}

func TestCLITeaProgramOptionsRunWithoutTTY(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "non-tty-*")
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()

	require.False(t, isTerminalFile(file))

	program := tea.NewProgram(quitTestModel{}, cliTeaProgramOptions(file, file)...)
	_, err = program.Run()
	require.NoError(t, err)
}
