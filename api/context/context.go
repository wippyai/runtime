// Package context is used to pass context between different parts of the application and not allocate
package context

import "context"

// Key represents a context key used for storing and retrieving values from the context.
// It provides a type-safe way to store context values using string names.
type Key struct {
	Name string
}

func (ck *Key) String() string {
	return ck.Name
}

// todo: move to individual packages and make proper accessor funcs
var (
	// EnvCtx represents the environment variables context key
	EnvCtx = &Key{Name: "env"} //nolint:gochecknoglobals
	// ValuesCtx represents the values storage context key
	ValuesCtx = &Key{Name: "values"} //nolint:gochecknoglobals
	// UowCtx represents the cleanup operations context key
	UowCtx = &Key{Name: "unit-of-work"} //nolint:gochecknoglobals
	// SecurityCtx represents the security settings context key
	SecurityCtx = &Key{Name: "security"} //nolint:gochecknoglobals
	// WakeUpKey represents a callback that can be used to notify process host about async process activity
	WakeUpKey = &Key{Name: "wakeup"} //nolint:gochecknoglobals
	// TerminalCtx represents the terminal manager context key
	TerminalCtx = &Key{Name: "terminal"} //nolint:gochecknoglobals
	// RunnerCtx represents the task group context key
	RunnerCtx = &Key{Name: "taskGroupKey"} //nolint:gochecknoglobals
)

// MergeContext combines values from a foreign context into a base context
func MergeContext(base, foreign context.Context) context.Context {
	// todo: security and values only
	// todO: redo
	// todo: this is termporary anchor
	return context.WithValue(base, EnvCtx, foreign.Value(EnvCtx))
	//return context.WithValue(base, "foreign", foreign)
}
