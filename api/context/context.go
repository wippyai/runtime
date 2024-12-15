// Package context is used to pass context between different parts of the application and not allocate
package context

type Key struct {
	name string
}

func (ck *Key) String() string {
	return ck.name
}

var (
	LoggerKey      = &Key{name: "logger"}      //nolint:gochecknoglobals
	CfgFilenameKey = &Key{name: "cfgfilename"} //nolint:gochecknoglobals
	// TODO: rename
	ContexterKey   = &Key{name: "contexter"}   //nolint:gochecknoglobals
	HttpHandlerKey = &Key{name: "httpHandler"} //nolint:gochecknoglobals
)

// TODO: rename
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
