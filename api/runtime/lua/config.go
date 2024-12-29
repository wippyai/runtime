package lua

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
)

const (
	KindFunction registry.Kind = "function.lua"
	KindLibrary  registry.Kind = "library.lua"
)

type (
	FunctionConfig struct {
		Meta      registry.Metadata `json:"meta"`
		Source    string            `json:"source"`
		Method    string            `json:"method"`
		Libraries []string          `json:"libraries"`
		Modules   []string          `json:"modules"`
		Pool      PoolConfig        `json:"pool"`
	}

	PoolConfig struct {
		Size int `json:"size"`
	}

	LibraryConfig struct {
		Meta    registry.Metadata `json:"meta"`
		Source  string            `json:"source"`
		Modules []string          `json:"modules"`
	}
)

func (c *FunctionConfig) Validate() error {
	if c.Source == "" {
		return fmt.Errorf("source is required")
	}

	if c.Method == "" {
		return fmt.Errorf("method is required")
	}

	if c.Pool.Size <= 0 {
		return fmt.Errorf("pool.num_vms must be greater than 0")
	}

	return nil
}
