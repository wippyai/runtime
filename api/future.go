package api

type future interface {
	*TaskResult | any
}

type Future[T future] interface {
	Await() T
	AwaitAll() T
}
