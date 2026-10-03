// SPDX-License-Identifier: MPL-2.0
//go:build windows

package terminal

import (
	"errors"
	"os"

	"github.com/muesli/cancelreader"
	"github.com/wippyai/runtime/internal/term/input"
)

// consoleInput adapts the console record reader to terminalInputReader. Its
// events come from ReadEvents; Read exists only to satisfy the interface.
type consoleInput struct {
	*input.ConsoleReader
}

func (consoleInput) Read([]byte) (int, error) {
	return 0, errors.New("console input delivers records, not bytes")
}

// newTerminalInputReader reads console records when stdin is a console and a
// byte stream when it is redirected.
func newTerminalInputReader(stdin *os.File, _ string) (terminalInputReader, error) {
	console, err := input.NewConsoleReader(stdin, false)
	if err == nil {
		return consoleInput{console}, nil
	}
	if !errors.Is(err, input.ErrNotConsole) {
		return nil, err
	}
	return cancelreader.NewReader(stdin)
}
