package lua

import (
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
	}

	LibraryConfig struct {
		Meta    registry.Metadata `json:"meta"`
		Source  string            `json:"source"`
		Modules []string          `json:"modules"`
	}
)
