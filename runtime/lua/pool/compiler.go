package pool

import (
	"github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/pool/queued"
	"github.com/ponyruntime/pony/runtime/lua/pool/sync"
	"go.uber.org/zap"
)

type Compiler struct {
	log *zap.Logger
}

func NewCompiler(log *zap.Logger) *Compiler {
	return &Compiler{
		log: log,
	}
}

func (c *Compiler) Compile(
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
