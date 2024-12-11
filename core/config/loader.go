package config

import (
	"github.com/ponyruntime/pony/api/config"
	"github.com/ponyruntime/pony/api/payload"
)

type Loader interface {
	WithPrefix(prefix config.ID) Loader
	Load(payload payload.Payload) (config.Entry, error)
}
