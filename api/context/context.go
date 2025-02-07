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

var (
	// BusCtx is the context key for the application's message bus
	BusCtx = &Key{Name: "bus"} //nolint:gochecknoglobals
	// TranscoderCtx is the context key for the media transcoder
	TranscoderCtx = &Key{Name: "transcoder"} //nolint:gochecknoglobals
	// ExecutorCtx is the context key for the task executor
	ExecutorCtx = &Key{Name: "executor"} //nolint:gochecknoglobals
	ProcessCtx  = &Key{Name: "workflow"} //nolint:gochecknoglobals
	// LoggerCtx is the context key for the application logger
	LoggerCtx = &Key{Name: "logger"} //nolint:gochecknoglobals
	// ValuesCtx is the context key for storing arbitrary values
	ValuesCtx = &Key{Name: "values"} //nolint:gochecknoglobals
	// CleanupCtx is the context key for cleanup operations
	CleanupCtx = &Key{Name: "cleanup"} //nolint:gochecknoglobals
	// TaskCtx is the context key for task-related data
	TaskCtx     = &Key{Name: "task"}         //nolint:gochecknoglobals
	RunnerCtx   = &Key{Name: "taskgroupkey"} //nolint:gochecknoglobals
	AsyncCtx    = &Key{Name: "schedulekey"}  //nolint:gochecknoglobals
	EnvCtx      = &Key{Name: "env"}          //nolint:gochecknoglobals
	SecurityCtx = &Key{Name: "security"}     //nolint:gochecknoglobals
	MetricsCtx  = &Key{Name: "metrics"}      //nolint:gochecknoglobals

	// external services
	TemporalCtx = &Key{Name: "temporal"} //nolint:gochecknoglobals
)
