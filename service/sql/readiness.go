// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/system/eventbus"
)

// awaitResource subscribes before publishing, and accepts only the outcome of
// this operation. The resource registry acknowledges after updating routing;
// a delete does not wait for existing borrowers to release their handles.
func (m *Manager) awaitResource(ctx context.Context, request event.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !m.bus.HasSubscribers(resource.System, request.Kind) {
		return fmt.Errorf("SQL resource coordination unavailable: %s", request.Kind)
	}
	opID := "sql-resource-" + uuid.NewString()
	if entry, ok := request.Data.(resource.Entry); ok {
		entry.OpID = opID
		request.Data = entry
	} else {
		request.Path = opID
	}
	replies := make(chan event.Event, 1)
	sub, err := eventbus.NewSubscriber(ctx, m.bus, resource.System, "resource.(accept|reject)", func(reply event.Event) {
		if reply.Path == opID {
			select {
			case replies <- reply:
			default:
			}
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe SQL resource outcome: %w", err)
	}
	defer sub.Close()
	m.bus.Send(ctx, request)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case reply := <-replies:
			if reply.Path != opID {
				continue
			}
			if reply.Kind == resource.Accept {
				return nil
			}
			if cause, ok := reply.Data.(error); ok {
				return fmt.Errorf("SQL resource %s rejected: %w", request.Kind, cause)
			}
			return fmt.Errorf("SQL resource %s rejected: %v", request.Kind, reply.Data)
		}
	}
}
