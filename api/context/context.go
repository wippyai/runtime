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
	// FunctionsCtx is the context key for the task executor
	FunctionsCtx  = &Key{Name: "functions"} //nolint:gochecknoglobals
	PrototypesCtx = &Key{Name: "processes"} //nolint:gochecknoglobals
	HostsCtx      = &Key{Name: "hosts"}     //nolint:gochecknoglobals

	// LoggerCtx is the context key for the application logger
	LoggerCtx = &Key{Name: "logger"} //nolint:gochecknoglobals
	// ValuesCtx is the context key for storing arbitrary values
	ValuesCtx = &Key{Name: "values"} //nolint:gochecknoglobals
	// CleanupCtx is the context key for cleanup operations
	CleanupCtx   = &Key{Name: "cleanup"}      //nolint:gochecknoglobals
	RunnerCtx    = &Key{Name: "taskgroupkey"} //nolint:gochecknoglobals
	AsyncCtx     = &Key{Name: "schedulekey"}  //nolint:gochecknoglobals
	EnvCtx       = &Key{Name: "env"}          //nolint:gochecknoglobals
	SecurityCtx  = &Key{Name: "security"}     //nolint:gochecknoglobals
	MetricsCtx   = &Key{Name: "metrics"}      //nolint:gochecknoglobals
	TemporalCtx  = &Key{Name: "temporal"}     //nolint:gochecknoglobals
	HandlerCtx   = &Key{Name: "handler"}      //nolint:gochecknoglobals
	RegistryCtx  = &Key{Name: "registry"}     //nolint:gochecknoglobals
	ResourcesCtx = &Key{Name: "resources"}    //nolint:gochecknoglobals
)
