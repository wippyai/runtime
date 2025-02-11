package factory

import (
	"github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/pool/queued"
	"github.com/ponyruntime/pony/runtime/lua/pool/sync"
	"go.uber.org/zap"
)

// Factory creates appropriate pool implementations based on configuration.
// It supports both queued and synchronized pool types.
type Factory struct {
	log *zap.Logger
}

// NewFactory creates a new pool factory with the specified logger
func NewFactory(log *zap.Logger) *Factory {
	return &Factory{
		log: log,
	}
}

// Build creates a new pool based on the provided configuration.
// It returns a queued pool if workers are specified, otherwise a synchronized pool.
// The factory parameter provides VM creation capabilities, while luaCfg determines pool settings.
func (c *Factory) Build(
	factory lua.Factory,
	luaCfg *lua.FunctionConfig,
) (lua.Callable, error) {
	if luaCfg.Pool.Workers > 0 {
		return queued.NewPool(
			factory,
			queued.WithSize(luaCfg.Pool.Size),
			queued.WithLogger(c.log),
			queued.WithWorkers(luaCfg.Pool.Workers),
		)
	}

	return sync.NewPool(
		factory,
		sync.WithSize(luaCfg.Pool.Size),
		sync.WithLogger(c.log),
	)
}
