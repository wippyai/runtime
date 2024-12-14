package tasks

import "github.com/ponyruntime/pony/api/tasks"

type future interface {
	*tasks.Result | any
}

type Future[T future] interface {
	Await() T
	AwaitAll() T
}
