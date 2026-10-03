// SPDX-License-Identifier: MPL-2.0
//go:build !windows

package terminal

import (
	"os"

	"github.com/muesli/cancelreader"
)

func newTerminalInputReader(stdin *os.File, _ string) (terminalInputReader, error) {
	return cancelreader.NewReader(stdin)
}
