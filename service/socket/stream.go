// SPDX-License-Identifier: MPL-2.0
package socket

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
)

func (d *Dispatcher) handleStreamWait(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c, ok := cmd.(*socketapi.StreamWaitCmd)
	if !ok || c.Run == nil {
		return errors.New("stream wait requires a continuation")
	}
	go func() {
		result := c.Run(ctx)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, result, nil)
		}
	}()
	return nil
}
