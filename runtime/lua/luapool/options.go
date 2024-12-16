package pool

import (
	"github.com/ponyruntime/pony/api/runtime"
	"time"
)

type Options func(*Pool)

func WithPollTimeout(timeout time.Duration) Options {
	return func(p *Pool) {
		p.timeout = timeout
	}
}

func WithModules(modules ...runtime.LuaModule) Options {
	return func(p *Pool) {
		p.modules = modules
	}
}
