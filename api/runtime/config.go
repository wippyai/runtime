package runtime

import (
	"errors"
	"github.com/ponyruntime/pony/api/registry"
)

const (
	KindFunction registry.Kind = "function"
	KindLibrary  registry.Kind = "library"

	RuntimeTag = "runtime"
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
		Sources []string          `json:"sources"`
		Modules []string          `json:"modules"`
	}
)

func (f *FunctionConfig) Validate() error {
	if f.Meta.StringValue(RuntimeTag) != "" {
		return nil
	}

	if f.Source == "" {
		return errors.New("missing source value")
	}

	if f.Method == "" {
		return errors.New("missing method value")
	}

	return errors.New("missing runtime meta value")
}
