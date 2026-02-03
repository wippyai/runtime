package code

import (
	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/types/io"
)

// CompileOptionsForManifest builds compile options that embed manifest type info.
// When manifest is nil, returns empty options.
func CompileOptionsForManifest(manifest *io.Manifest) (glua.CompileOptions, error) {
	if manifest == nil {
		return glua.CompileOptions{}, nil
	}
	data, err := manifest.Encode()
	if err != nil {
		return glua.CompileOptions{}, err
	}
	typeNames := make(map[string]struct{}, len(manifest.Types))
	for name := range manifest.Types {
		if name == "" {
			continue
		}
		typeNames[name] = struct{}{}
	}
	return glua.CompileOptions{TypeInfo: data, TypeNames: typeNames}, nil
}
