// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
)

func (d *Dispatcher) handlePollWait(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	c, ok := cmd.(*socketapi.PollWaitCmd)
	if !ok || c.Wait == nil {
		return errors.New("socket poll requires a wait operation")
	}
	go func() {
		indexes, err := c.Wait(ctx)
		if ctx.Err() == nil {
			receiver.CompleteYield(tag, indexes, err)
		}
	}()
	return nil
}
