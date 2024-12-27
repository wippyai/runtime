// Package context is used to pass context between different parts of the application and not allocate
package context

type Key struct {
	Name string
}

func (ck *Key) String() string {
	return ck.Name
}

var (
	// todo: how can we declare context keys outside of this file?
	LoggerKey      = &Key{Name: "logger"}      //nolint:gochecknoglobals
	CfgFilenameKey = &Key{Name: "cfgfilename"} //nolint:gochecknoglobals
	// TODO: rename
	ContexterKey   = &Key{Name: "contexter"}   //nolint:gochecknoglobals
	HttpHandlerKey = &Key{Name: "httpHandler"} //nolint:gochecknoglobals
	RouteInfoCtx   = &Key{Name: "routeInfo"}   //nolint:gochecknoglobals
	RequestCtx     = &Key{Name: "httpRequest"} //nolint:gochecknoglobals
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
