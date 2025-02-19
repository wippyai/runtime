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
	// --- Core layers
	BusCtx        = &Key{Name: "bus"}        //nolint:gochecknoglobals
	TranscoderCtx = &Key{Name: "transcoder"} //nolint:gochecknoglobals
	FunctionsCtx  = &Key{Name: "functions"}  //nolint:gochecknoglobals
	ProcessesCtx  = &Key{Name: "processes"}  //nolint:gochecknoglobals
	RegistryCtx   = &Key{Name: "registry"}   //nolint:gochecknoglobals

	// --- System services
	MetricsCtx   = &Key{Name: "metrics"}   //nolint:gochecknoglobals
	ResourcesCtx = &Key{Name: "resources"} //nolint:gochecknoglobals

	// -- Enviroment and boundaries
	EnvCtx  = &Key{Name: "env"}  //nolint:gochecknoglobals
	NodeCtx = &Key{Name: "node"} //nolint:gochecknoglobals
	HostCtx = &Key{Name: "host"} //nolint:gochecknoglobals

	// --- Execution path specific
	IDCtx       = &Key{Name: "handler"}  //nolint:gochecknoglobals
	ValuesCtx   = &Key{Name: "values"}   //nolint:gochecknoglobals
	CleanupCtx  = &Key{Name: "cleanup"}  //nolint:gochecknoglobals
	SecurityCtx = &Key{Name: "security"} //nolint:gochecknoglobals

	// --- Contains a callack that is when not empty can be used to notify process host about async
	// --- process activity to wake up process thread
	WakeUpKey = &Key{Name: "wakeup"} //nolint:gochecknoglobals

	// --- Runtime and lifecycle specific
	TerminalCtx = &Key{Name: "terminal"}     //nolint:gochecknoglobals
	RunnerCtx   = &Key{Name: "taskGroupKey"} //nolint:gochecknoglobals
	AsyncCtx    = &Key{Name: "scheduleKey"}  //nolint:gochecknoglobals
	TemporalCtx = &Key{Name: "temporal"}     //nolint:gochecknoglobals
	LoggerCtx   = &Key{Name: "logger"}       //nolint:gochecknoglobals
)

func MergeContext(base, foreign context.Context) context.Context {
	// todo: security and values only

	return context.WithValue(base, "foreign", foreign)
}
