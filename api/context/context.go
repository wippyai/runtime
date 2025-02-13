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
	BusCtx        = &Key{Name: "bus"}          //nolint:gochecknoglobals
	TranscoderCtx = &Key{Name: "transcoder"}   //nolint:gochecknoglobals
	FunctionsCtx  = &Key{Name: "functions"}    //nolint:gochecknoglobals
	ProcessesCtx  = &Key{Name: "processes"}    //nolint:gochecknoglobals
	LoggerCtx     = &Key{Name: "logger"}       //nolint:gochecknoglobals
	ValuesCtx     = &Key{Name: "values"}       //nolint:gochecknoglobals
	CleanupCtx    = &Key{Name: "cleanup"}      //nolint:gochecknoglobals
	RunnerCtx     = &Key{Name: "taskgroupkey"} //nolint:gochecknoglobals
	AsyncCtx      = &Key{Name: "schedulekey"}  //nolint:gochecknoglobals
	EnvCtx        = &Key{Name: "env"}          //nolint:gochecknoglobals
	SecurityCtx   = &Key{Name: "security"}     //nolint:gochecknoglobals
	MetricsCtx    = &Key{Name: "metrics"}      //nolint:gochecknoglobals
	TemporalCtx   = &Key{Name: "temporal"}     //nolint:gochecknoglobals
	HandlerCtx    = &Key{Name: "handler"}      //nolint:gochecknoglobals
	RegistryCtx   = &Key{Name: "registry"}     //nolint:gochecknoglobals
	ResourcesCtx  = &Key{Name: "resources"}    //nolint:gochecknoglobals
	TerminalCtx   = &Key{Name: "terminal"}
)
