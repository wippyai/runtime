// Package context is used to pass context between different parts of the application and not allocate
package context

// KeyScope defines the propagation behavior of context keys
type KeyScope int

const (
	// ScopeCall keys are call-specific: write-once, not inherited by child contexts
	// Examples: PID, cancel function, callbacks
	ScopeCall KeyScope = iota

	// ScopeThread keys thread through call chains: mutable, inherited by child contexts
	// Examples: security context, options, user values
	ScopeThread
)

// Key represents a context key used for storing and retrieving values from the context.
// It provides a type-safe way to store context values using string names.
type Key struct {
	Name  string
	Scope KeyScope
}

func (ck *Key) String() string {
	return ck.Name
}

// wakeupKey is the context key for UnitOfWork wakeup callbacks
var wakeupKey = &Key{Name: "wakeup", Scope: ScopeCall}

// WakeUpKey is the public accessor for the wakeup context key
// Represents a callback that can be used to notify process host about async process activity
var WakeUpKey = wakeupKey
