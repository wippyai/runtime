package context

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

// Iterate calls the provided function for each key-value pair in the contexter.
func (c *Contexter[T]) Iterate(fn func(key string, value T)) {
	for k, v := range c.shared {
		fn(k, v)
	}
}

func (c *Contexter[T]) Len() int {
	return len(c.shared)
}
