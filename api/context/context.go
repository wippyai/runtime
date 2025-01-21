// Package context is used to pass context between different parts of the application and not allocate
package context

type Key struct {
	Name string
}

func (ck *Key) String() string {
	return ck.Name
}

var (
	BusCtx        = &Key{Name: "bus"}         //nolint:gochecknoglobals
	TranscoderCtx = &Key{Name: "transcoder"}  //nolint:gochecknoglobals
	ExecutorCtx   = &Key{Name: "executor"}    //nolint:gochecknoglobals
	LoggerCtx     = &Key{Name: "logger"}      //nolint:gochecknoglobals
	ValuesCtx     = &Key{Name: "values"}      //nolint:gochecknoglobals
	CleanupCtx    = &Key{Name: "cleanup"}     //nolint:gochecknoglobals
	TaskCtx       = &Key{Name: "task"}        //nolint:gochecknoglobals
	LFSUserData   = &Key{Name: "lfsuserdata"} //nolint:gochecknoglobals
)

type Contexter[T any] struct {
	shared map[string]T
}

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
