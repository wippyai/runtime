// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// AwaitResourceUpdate repoints the resource registry at a replacement provider
// and returns once the registry reports that operation applied.
//
// Store updates need this rather than a bare Send. The registry and the
// supervisor are independent subscribers, so publishing the replacement gives
// no ordering against the supervisor retiring the superseded instance at
// commit. Returning only once the registry serves the replacement puts the
// repoint strictly before the commit that stops the old one, and turns a
// dropped event into a failed entry operation instead of a silent half-update.
//
// The wait is correlated by a per-operation id rather than by resource path:
// registrations and updates for one resource share a path, so a path-keyed wait
// could be satisfied by the outcome of a different, possibly earlier operation.
func AwaitResourceUpdate(ctx context.Context, bus event.Bus, entry resource.Entry, action string) error {
	opID, err := newOperationID()
	if err != nil {
		return NewResourceHandoverError(action, err)
	}
	entry.OpID = opID

	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		return NewResourceHandoverError(action, ErrResourceCoordinationUnavailable)
	}

	waiter, err := awaitSvc.Prepare(ctx, resource.System, "resource.(accept|reject)", opID, 0)
	if err != nil {
		return NewResourceHandoverError(action, err)
	}

	bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Update,
		Path:   entry.ID.String(),
		Data:   entry,
	})

	result := waiter.Wait()
	if result.Error != nil {
		return NewResourceHandoverError(action, result.Error)
	}
	if !result.Accepted {
		return NewResourceHandoverError(action, fmt.Errorf("%v", result.Event.Data))
	}
	return nil
}

// newOperationID returns a value unique to one resource operation, used to
// correlate its outcome.
func newOperationID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "resource-op-" + hex.EncodeToString(buf[:]), nil
}
