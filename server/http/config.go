package http

import "github.com/ponyruntime/pony/api/registry"

type (
	ServerConfig struct {
		Listen string `json:"listen" yaml:"listen"`
	}

	EndpointConfig struct {
		Path   string        `json:"path" yaml:"path"`
		Method string        `json:"method" yaml:"method"`
		Target registry.Path `json:"target" yaml:"target"`
	}
)
