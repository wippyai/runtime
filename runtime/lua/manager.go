package lua

import (
	"github.com/ponyruntime/pony/api/events"
	"go.uber.org/zap"
)

type Runtime struct {
}

func NewRuntimeManager(bus events.Bus, zap *zap.Logger) *Runtime {
	return &Runtime{}
}
