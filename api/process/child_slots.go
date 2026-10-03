// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"sync/atomic"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
)

// ErrChildLimitExceeded fails a spawn that would exceed the parent's child
// limit.
var ErrChildLimitExceeded = apierror.New(LimitExceeded, "process child limit exceeded").WithRetryable(apierror.False)

var (
	// childSlotsKey holds a process's child limit. Processes it starts share
	// the limit: it bounds the whole tree of processes started under it, so
	// a child cannot start more than its ancestor was allowed.
	childSlotsKey = &ctxapi.Key{Name: "process.child_slots", Inherit: true, Execution: true}
	childSlotKey  = &ctxapi.Key{Name: "process.child_slot"}
)

// ChildSlots bounds how many child processes a process and its descendants
// may have running at once. A spawn reserves a slot as a frame attachment of the child: it is
// released when the child completes, or rolled back when the spawn fails.
type ChildSlots struct {
	limit int64
	used  atomic.Int64
}

// NewChildSlots returns a limit of max running children.
func NewChildSlots(limit int) *ChildSlots {
	return &ChildSlots{limit: int64(limit)}
}

// InUse returns the number of reserved slots.
func (c *ChildSlots) InUse() int {
	return int(c.used.Load())
}

func (c *ChildSlots) reserve() (*childSlot, error) {
	for {
		used := c.used.Load()
		if used >= c.limit {
			return nil, ErrChildLimitExceeded
		}
		if c.used.CompareAndSwap(used, used+1) {
			return &childSlot{slots: c}, nil
		}
	}
}

// ChildSlotsPair installs a child limit on a process frame.
func ChildSlotsPair(slots *ChildSlots) ctxapi.Pair {
	return ctxapi.Pair{Key: childSlotsKey, Value: slots}
}

// ChildSlotResolver is a frame resolver that reserves a child slot when a
// process with a child limit spawns a child process.
func ChildSlotResolver(ctx context.Context, options attrs.Attributes) ([]ctxapi.Pair, error) {
	if options == nil {
		return nil, nil
	}
	if _, child := options.Get(ProcessParentKey); !child {
		return nil, nil
	}
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, nil
	}
	val, ok := fc.Get(childSlotsKey)
	if !ok {
		return nil, nil
	}
	slots, ok := val.(*ChildSlots)
	if !ok {
		return nil, nil
	}
	slot, err := slots.reserve()
	if err != nil {
		return nil, err
	}
	return []ctxapi.Pair{{Key: childSlotKey, Value: slot}}, nil
}

// childSlot is one reserved slot; releasing it twice is a no-op.
type childSlot struct {
	slots    *ChildSlots
	released atomic.Bool
}

// Complete releases the slot when the child completes.
func (s *childSlot) Complete() {
	s.release()
}

func (s *childSlot) Close() error {
	s.release()
	return nil
}

func (s *childSlot) Rollback() error {
	s.release()
	return nil
}

func (s *childSlot) release() {
	if s.released.CompareAndSwap(false, true) {
		s.slots.used.Add(-1)
	}
}
