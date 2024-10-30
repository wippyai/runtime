package pool

import (
	"time"

	"github.com/ponyruntime/pony/api"
)

type Options func(*Pool)

func WithNumWorkers(numWorkers int) Options {
	return func(p *Pool) {
		p.numWorkers = numWorkers
	}
}

func WithPollTimeout(timeout time.Duration) Options {
	return func(p *Pool) {
		p.timeout = timeout
	}
}

func WithModules(modules ...api.Module) Options {
	return func(p *Pool) {
		p.modules = modules
	}
}
