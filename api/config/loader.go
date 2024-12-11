package config

import (
	"github.com/ponyruntime/pony/api/payload"
)

type (
	Loader interface {
		WithPrefix(ID) Loader
		Load(...payload.Payload) error
		Entries() []Entry
		Reset()
	}
)
