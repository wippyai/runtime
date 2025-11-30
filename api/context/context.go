// Package context is used to pass context between different parts of the application and not allocate
package context

import (
	"context"
	"errors"
)

// ErrNoFrameContext is returned when trying to set a frame value without a frame context
var ErrNoFrameContext = errors.New("no frame context available")

// ErrNoAppContext is returned when trying to set an app value without an app context
var ErrNoAppContext = errors.New("no app context available")

// Key represents a context key used for storing and retrieving values from the context.
// It provides a type-safe way to store context values using string names.
// When Inherit is true, the value will be automatically copied to new frames
// created from sealed parent frames.
type Key struct {
	Name    string
	Inherit bool // Auto-copy to child frames when parent sealed
}

func (ck *Key) String() string {
	return ck.Name
}

// wakeupKey is the context key for UnitOfWork wakeup callbacks
var wakeupKey = &Key{Name: "wakeup"}

// WakeUpKey is the public accessor for the wakeup context key
// Represents a callback that can be used to notify process host about async process activity
var WakeUpKey = wakeupKey

// SetWakeUp stores a wakeup callback in the FrameContext.
// Returns error if no frame context or frame is sealed.
func SetWakeUp(ctx context.Context, fn func()) error {
	fc := FrameFromContext(ctx)
	if fc == nil {
		return ErrNoFrameContext
	}
	return fc.Set(wakeupKey, fn)
}

// GetWakeUp retrieves the wakeup callback from the FrameContext.
// Returns nil if not found.
func GetWakeUp(ctx context.Context) func() {
	fc := FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(wakeupKey); ok {
		if fn, ok := val.(func()); ok {
			return fn
		}
	}
	return nil
}
