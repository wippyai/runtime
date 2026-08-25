// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/resource"
)

// ErrResourceCoordinationUnavailable reports that the resource handover cannot
// be confirmed, so a store update must not claim the replacement is being
// served.
var ErrResourceCoordinationUnavailable = apierror.New(apierror.Unavailable,
	"resource handover coordination unavailable").WithRetryable(apierror.True)

// SendAndAwaitResourceAck publishes a resource event and returns once the
// resource registry reports it applied.
//
// Store updates need this rather than a bare Send. The registry and the
// supervisor are independent subscribers, so publishing the replacement gives
// no ordering against the supervisor retiring the superseded instance at
// commit. Returning only once the registry serves the replacement puts the
// repoint strictly before the commit that stops the old one, and turns a
// dropped event into a failed entry operation instead of a silent half-update.
func SendAndAwaitResourceAck(ctx context.Context, bus event.Bus, evt event.Event, action string) error {
	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		return NewResourceHandoverError(action, ErrResourceCoordinationUnavailable)
	}

	waiter, err := awaitSvc.Prepare(ctx, resource.System, "resource.(accept|reject)", evt.Path, 0)
	if err != nil {
		return NewResourceHandoverError(action, err)
	}

	bus.Send(ctx, evt)

	result := waiter.Wait()
	if result.Error != nil {
		return NewResourceHandoverError(action, result.Error)
	}
	if !result.Accepted {
		return NewResourceHandoverError(action, fmt.Errorf("%v", result.Event.Data))
	}
	return nil
}
