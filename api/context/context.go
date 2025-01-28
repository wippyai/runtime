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
	// LoggerCtx is the context key for the application logger
	LoggerCtx = &Key{Name: "logger"} //nolint:gochecknoglobals
	// ValuesCtx is the context key for storing arbitrary values
	ValuesCtx = &Key{Name: "values"} //nolint:gochecknoglobals
	// CleanupCtx is the context key for cleanup operations
	CleanupCtx = &Key{Name: "cleanup"} //nolint:gochecknoglobals
	// TaskCtx is the context key for task-related data
	TaskCtx         = &Key{Name: "task"}         //nolint:gochecknoglobals
	TaskGroupKeyCtx = &Key{Name: "taskgroupkey"} //nolint:gochecknoglobals
	ScheduleKeyCtx  = &Key{Name: "schedulekey"}  //nolint:gochecknoglobals
)

// Contexter provides a generic type-safe context value store.
// It allows storing and retrieving values of type T using string keys.
type Contexter[T any] struct {
	shared map[string]T
}

// NewContexter creates a new instance of Contexter[T] with an initialized
// empty map for storing context values.
func NewContexter[T any]() *Contexter[T] {
	return &Contexter[T]{
		shared: make(map[string]T),
	}
}

func (c *Contexter[T]) WithValue(key string, value T) {
	c.shared[key] = value
}

func (c *Contexter[T]) Value(key string) (T, bool) {
	v, ok := c.shared[key]
	return v, ok
}
