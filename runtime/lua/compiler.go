package lua

import (
	"github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/pool"
	"github.com/ponyruntime/pony/runtime/lua/pool/sync"
	"go.uber.org/zap"
	"log"
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
	vmCfg *pool.VMConfig,
	luaCfg *lua.FunctionConfig,
) (lua.Callable, error) {
	log.Printf("luaCfg: %+v\n", luaCfg)
	log.Printf("vmCfg: %+v\n", vmCfg)
	return sync.NewPool(
		vmCfg,
		sync.WithSize(luaCfg.Pool.Size),
		sync.WithLogger(c.log),
	)
}
