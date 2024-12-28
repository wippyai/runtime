package lua

import (
	"github.com/ponyruntime/pony/api/runtime/lua"
	"go.uber.org/zap"
)

type Compiler struct {
	log     *zap.Logger
	modules map[string]lua.Module
}

func NewCompiler(log *zap.Logger, modules map[string]lua.Module) *Compiler {
	return &Compiler{
		log:     log,
		modules: modules,
	}
}

func (c *Compiler) Compile(
	id string,
	fn lua.FunctionConfig,
	libs []lua.LibraryConfig,
) (string, error) {
	c.log.Info(
		"compiling function",
		zap.String("name", id),
		zap.Any("function", fn),
		zap.Any("libraries", libs),
	)

	return "READY!", nil
}
