// SPDX-License-Identifier: MPL-2.0
//go:build !windows

package terminal

import (
	"context"
	"os"

	uv "github.com/charmbracelet/ultraviolet"
)

func newTerminalInputReader(stdin *os.File, _ string) (terminalInputReader, error) {
	return uv.NewCancelReader(stdin)
}

func streamTerminalInput(ctx context.Context, reader terminalInputReader, sink inputEventSink) error {
	framed := newFramedTerminalInput(reader)
	terminalReader := uv.NewTerminalReader(framed, os.Getenv("TERM"))
	events := make(chan uv.Event)
	streamDone := make(chan error, 1)
	go func() { streamDone <- terminalReader.StreamEvents(ctx, events) }()

	// StreamEvents sends synchronously. Keep receiving after cancellation so its
	// final flush cannot strand the decoder while Stop waits for this loop.
	contextDone := ctx.Done()
	stopping := false
	for {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			framed.acknowledgeEvent()
			if !stopping {
				sink(convertUVInputEvent(event))
			}
		case err := <-streamDone:
			if stopping || ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
			return framed.Err()
		case <-contextDone:
			stopping = true
			contextDone = nil
		}
	}
}
