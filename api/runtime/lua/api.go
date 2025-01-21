package lua

import (
	"context"
	"github.com/ponyruntime/pony/api/registry"
	lua "github.com/yuin/gopher-lua"
)

type (
	Module interface {
		Loader(*lua.LState) int
		Name() string
	}

	Callable interface {
		Execute(ctx context.Context, funcName string, args ...lua.LValue) (lua.LValue, error)
	}

	Factory interface {
		Compile() error
		MakeVM() (VM, error)
	}

	FunctionProvider interface {
		Get(name registry.ID) (*FunctionConfig, error)
		Has(name registry.ID) bool
	}

	LibraryRegistry interface {
		Get(name registry.ID) (*LibraryConfig, error)
		Has(name registry.ID) bool
	}

	ModuleRegistry interface {
		Get(name string) (Module, error)
		Has(name string) bool
	}

	VM interface {
		Execute(ctx context.Context, name string, args ...lua.LValue) (lua.LValue, error)
		Close()
	}
)
