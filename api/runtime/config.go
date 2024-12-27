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
		Source  string            `json:"source"`
		Modules []string          `json:"modules"`
	}
)

func (f *FunctionConfig) Validate() error {
	if f.Meta.StringValue(RuntimeTag) == "" {
		return errors.New("missing runtime meta value")
	}

	if f.Source == "" {
		return errors.New("missing source value")
	}

	if f.Method == "" {
		return errors.New("missing method value")
	}

	return nil
}

func (l *LibraryConfig) Validate() error {
	if l.Meta.StringValue(RuntimeTag) == "" {
		return errors.New("missing runtime meta value")
	}

	if l.Source == "" {
		return errors.New("missing source code")
	}

	return nil
}
