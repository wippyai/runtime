// SPDX-License-Identifier: MPL-2.0

package resource

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/system/eventbus"
)

// AwaitOperation subscribes before publishing, and accepts only the outcome of
// this operation. The resource registry acknowledges after updating routing;
// a delete does not wait for existing borrowers to release their handles.
// Cancellation is honored before dispatch. Once published, the routing change
// may already be applied, so finish the ownership handoff instead of abandoning
// its acknowledgement and leaving routing and manager state inconsistent.
func AwaitOperation(ctx context.Context, bus event.Bus, request event.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !bus.HasSubscribers(resource.System, request.Kind) {
		return fmt.Errorf("resource coordination unavailable: %s", request.Kind)
	}
	opID := "resource-" + uuid.NewString()
	if entry, ok := request.Data.(resource.Entry); ok {
		entry.OpID = opID
		request.Data = entry
	} else {
		request.Path = opID
	}
	replies := make(chan event.Event, 1)
	operationCtx := context.WithoutCancel(ctx)
	sub, err := eventbus.NewSubscriber(operationCtx, bus, resource.System, "resource.(accept|reject)", func(reply event.Event) {
		if reply.Path == opID {
			select {
			case replies <- reply:
			default:
			}
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe to resource outcome: %w", err)
	}
	defer sub.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	var busDone <-chan struct{}
	if bus, ok := bus.(interface{ Done() <-chan struct{} }); ok {
		busDone = bus.Done()
	}
	bus.Send(operationCtx, request)
	select {
	case <-busDone:
		return fmt.Errorf("resource coordination stopped: %w", eventbus.ErrBusClosed)
	case reply := <-replies:
		if reply.Kind == resource.Accept {
			return nil
		}
		if cause, ok := reply.Data.(error); ok {
			return fmt.Errorf("%s rejected: %w", request.Kind, cause)
		}
		return fmt.Errorf("%s rejected: %v", request.Kind, reply.Data)
	}
}
