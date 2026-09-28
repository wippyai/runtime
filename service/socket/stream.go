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
	if !ok || c == nil || c.Run == nil {
		return errors.New("stream wait requires a continuation")
	}
	waitCtx := ctx
	cancel := context.CancelFunc(func() {})
	if !c.Deadline.IsZero() {
		waitCtx, cancel = context.WithDeadline(ctx, c.Deadline)
	}
	go func() {
		defer cancel()
		result := c.Run(waitCtx)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, result, nil)
		}
	}()
	return nil
}
