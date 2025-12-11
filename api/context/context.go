// Package context provides application-level context management utilities.
// It includes AppContext for global key-value storage and FrameContext for
// hierarchical scoped values with parent-child relationships.
package context

import "github.com/wippyai/runtime/api/attrs"

type (
	// Key represents a context key used for storing and retrieving values.
	// When Inherit is true, the value will be automatically copied to new frames
	// created from sealed parent frames.
	Key struct {
		Name    string
		Inherit bool
	}

	// Pair represents a key-value pair for batch operations.
	Pair struct {
		Key   any
		Value any
	}

	// Cloner is implemented by types that can create a copy of themselves.
	// Used during frame inheritance to prevent shared mutable state.
	Cloner interface {
		Clone() any
	}

	// Closer is implemented by values that need cleanup when frame is released.
	Closer interface {
		Close() error
	}

	// CloserFunc is a function that implements Closer interface.
	CloserFunc func() error

	// Values is an alias to attrs.Bag for storing arbitrary key-value pairs.
	Values = attrs.Bag
)

// String returns the key name.
func (ck *Key) String() string { return ck.Name }

// Close implements Closer interface.
func (f CloserFunc) Close() error { return f() }

// NewValues creates a new Values instance.
func NewValues() Values {
	return attrs.NewBag()
}
