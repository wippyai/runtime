// SPDX-License-Identifier: MPL-2.0

package boot

// ConfigLayers returns configuration inputs in increasing precedence order.
// Consumers resolving aliases after entry kinds become known need this order;
// Get and Keys continue to expose the ordinary merged configuration.
// The returned slice is owned by the caller. Configurations are read-only views.
func ConfigLayers(cfg Config) []Config {
	if cfg == nil {
		return nil
	}
	if layered, ok := cfg.(*layeredConfig); ok {
		layers := append([]Config(nil), layered.layers...)
		if layered.scope != "" {
			for i, layer := range layers {
				layers[i] = layer.Sub(layered.scope[:len(layered.scope)-len(ConfigSep)])
			}
		}
		return layers
	}
	return []Config{cfg}
}

// WithConfigLayers attaches ordered input provenance to a resolved config.
// It does not merge values or change their existing precedence semantics.
func WithConfigLayers(resolved Config, inputs ...Config) Config {
	if resolved == nil {
		return nil
	}
	var layers []Config
	for _, input := range inputs {
		layers = append(layers, ConfigLayers(input)...)
	}
	if len(layers) == 0 {
		return resolved
	}
	return &layeredConfig{Config: resolved, layers: layers}
}

type layeredConfig struct {
	Config
	scope  string
	layers []Config
}

func (c *layeredConfig) Sub(prefix string) Config {
	// Retain the separator, including for an empty prefix, exactly as the
	// underlying config.Sub does. Empty scope then means no Sub was applied.
	scope := c.scope + prefix + ConfigSep
	// Ordinary scoped reads do not materialize every historical input. Only
	// consumers explicitly requesting ConfigLayers pay for the scoped views.
	return &layeredConfig{Config: c.Config.Sub(prefix), layers: c.layers, scope: scope}
}
