// Package context is used to pass context between different parts of the application and not allocate
package context

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
	EnvCtx = &Key{Name: "env"}
	// ValuesCtx represents the values storage context key
	ValuesCtx = &Key{Name: "values"}
	// WakeUpKey represents a callback that can be used to notify process host about async process activity
	WakeUpKey = &Key{Name: "wakeup"}
)
