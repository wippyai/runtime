package helpers

import (
	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/runtime"
)

type Register struct {
}

func (r *Register) AddLibrary(registry.ID, config.LibraryConfig) error {
	return nil
}

func (r *Register) UpdateLibrary(registry.ID, config.LibraryConfig) error {
	return nil
}

func (r *Register) AddFunction(registry.ID, config.FunctionConfig) error {
	return nil
}

func (r *Register) UpdateFunction(registry.ID, config.FunctionConfig) error {
	return nil
}

func (r *Register) Delete(registry.ID) error {
	return nil
}
