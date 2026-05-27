// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/event"
	queueapi "github.com/wippyai/runtime/api/queue"
)

// SendAndAwaitManagerAck sends a queue-system command and waits until the
// queue Manager has accepted or rejected that exact path. Registry entry
// handlers use this at apply boundaries so "entry applied" means the manager's
// observable state is ready for dependent entries, not merely that an async
// event was published.
func SendAndAwaitManagerAck(ctx context.Context, bus event.Bus, evt event.Event, action string) error {
	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		return NewConfigError(action+" response coordination unavailable", nil)
	}

	waiter, err := awaitSvc.Prepare(ctx, queueapi.System, "queue.(accept|reject)", evt.Path, 0)
	if err != nil {
		return NewConfigError("failed to subscribe to "+action+" response", err)
	}

	bus.Send(ctx, evt)

	result := waiter.Wait()
	if result.Error != nil {
		return NewConfigError(action+" rejected", result.Error)
	}
	if !result.Accepted {
		return NewConfigError(action+" rejected", fmt.Errorf("%v", result.Event.Data))
	}
	return nil
}
