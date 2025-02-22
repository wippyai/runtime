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

var (
	// BusCtx represents the event bus context key for the application
	BusCtx = &Key{Name: "bus"} //nolint:gochecknoglobals
	// TranscoderCtx represents the transcoder service context key
	TranscoderCtx = &Key{Name: "transcoder"} //nolint:gochecknoglobals
	// FunctionsCtx represents the function registry context key
	FunctionsCtx = &Key{Name: "functions"} //nolint:gochecknoglobals
	// ProcessesCtx represents the process manager context key
	ProcessesCtx = &Key{Name: "processes"} //nolint:gochecknoglobals
	// RegistryCtx represents the registry service context key
	RegistryCtx = &Key{Name: "registry"} //nolint:gochecknoglobals
	// MetricsCtx represents the metrics service context key
	MetricsCtx = &Key{Name: "metrics"} //nolint:gochecknoglobals
	// ResourcesCtx represents the resource manager context key
	ResourcesCtx = &Key{Name: "resources"} //nolint:gochecknoglobals
	// EnvCtx represents the environment variables context key
	EnvCtx = &Key{Name: "env"} //nolint:gochecknoglobals
	// NodeCtx represents the node manager context key
	NodeCtx = &Key{Name: "node"} //nolint:gochecknoglobals
	// HostCtx represents the host manager context key
	HostCtx = &Key{Name: "host"} //nolint:gochecknoglobals
	// ProcessCtx represents the process context key (contains process control api)
	ProcessCtx = &Key{Name: "process"} //nolint:gochecknoglobals
	// FunctionCtx represents the function context key
	FunctionCtx = &Key{Name: "function"}
	// ValuesCtx represents the values storage context key
	ValuesCtx = &Key{Name: "values"} //nolint:gochecknoglobals
	// CleanupCtx represents the cleanup operations context key
	CleanupCtx = &Key{Name: "cleanup"} //nolint:gochecknoglobals
	// SecurityCtx represents the security settings context key
	SecurityCtx = &Key{Name: "security"} //nolint:gochecknoglobals
	// WakeUpKey represents a callback that can be used to notify process host about async process activity
	WakeUpKey = &Key{Name: "wakeup"} //nolint:gochecknoglobals
	// TerminalCtx represents the terminal manager context key
	TerminalCtx = &Key{Name: "terminal"} //nolint:gochecknoglobals
	// RunnerCtx represents the task group context key
	RunnerCtx = &Key{Name: "taskGroupKey"} //nolint:gochecknoglobals
	// AsyncCtx represents the scheduler context key
	AsyncCtx = &Key{Name: "scheduleKey"} //nolint:gochecknoglobals
	// LoggerCtx represents the logger context key
	LoggerCtx = &Key{Name: "logger"} //nolint:gochecknoglobals
	// FS Registry
	FSRegistryCtx = &Key{Name: "fs"}
)

// MergeContext combines values from a foreign context into a base context
func MergeContext(base, foreign context.Context) context.Context {
	// todo: security and values only
	// todO: redo
	return context.WithValue(base, EnvCtx, foreign.Value(EnvCtx))
	//return context.WithValue(base, "foreign", foreign)
}
