package futures

import (
	"time"
)

// Future is a struct that represents a future
// I is the input
// O is the output
type Future[T any, O chan any] struct {
	timeout time.Duration
	task    T
	out     O
}

func NewFuture[T any, O chan any](task T, out O, timeout time.Duration) *Future[T, O] {
	return &Future[T, O]{
		timeout,
		task,
		out,
	}
}

func (f *Future[T, O]) Start() T {
	go func() {
		f.out <- f.out
	}()

	// return the task itself to wait
	return f.task
}

func (f *Future[T, O]) Await() O {
	select {
	case val := <-f.out:
		return val.(O)
	case <-time.After(f.timeout):
		panic("timeout")
	}
}

func (f *Future[T, O]) AwaitAll() O {
	return f.out
}
