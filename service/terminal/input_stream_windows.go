// SPDX-License-Identifier: MPL-2.0
//go:build windows

package terminal

import (
	"context"
	"errors"
	"os"

	"github.com/charmbracelet/x/input"
	"github.com/muesli/cancelreader"
)

// Keep the established x/input Console API reader on Windows. Ultraviolet's
// Windows stream selects native console records through an unexported concrete
// reader type, which a bounded io.Reader wrapper would hide.
func newTerminalInputReader(stdin *os.File, termType string) (terminalInputReader, error) {
	return input.NewReader(stdin, termType, 0)
}

func streamTerminalInput(ctx context.Context, reader terminalInputReader, sink inputEventSink) error {
	legacy, ok := reader.(*input.Reader)
	if !ok {
		return errors.New("windows terminal input reader is not x/input")
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		events, err := legacy.ReadEvents()
		for _, event := range events {
			sink(ConvertInputEvent(event))
		}
		if err == nil {
			continue
		}
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, cancelreader.ErrCanceled) {
			return nil
		}
		return err
	}
}
