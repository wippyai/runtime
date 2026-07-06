// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

func newCLIProgram(model tea.Model) *tea.Program {
	return tea.NewProgram(model, cliTeaProgramOptions(os.Stdin, os.Stdout)...)
}

func cliTeaProgramOptions(input, output *os.File) []tea.ProgramOption {
	if !isTerminalFile(input) || !isTerminalFile(output) {
		return []tea.ProgramOption{
			tea.WithInput(nil),
			tea.WithoutRenderer(),
		}
	}
	return nil
}

func isTerminalFile(file *os.File) bool {
	return file != nil && term.IsTerminal(int(file.Fd()))
}
