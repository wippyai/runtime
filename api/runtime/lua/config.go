package lua

import (
	"fmt"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"

	"github.com/ponyruntime/pony/api/registry"
)

const (
	KindFunction registry.Kind = "function.lua"
	KindLibrary  registry.Kind = "library.lua"
	KindTerminal registry.Kind = "terminal.lua"
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

	TerminalConfig struct {
		Meta      registry.Metadata          `json:"meta"`
		Source    string                     `json:"source"`
		Method    string                     `json:"method"`
		Libraries []string                   `json:"libraries"`
		Modules   []string                   `json:"modules"`
		Options   terminal.Options           `json:"options"`
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
	}

	PoolConfig struct {
		Size    int `json:"size"`
		Workers int `json:"workers"`
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
