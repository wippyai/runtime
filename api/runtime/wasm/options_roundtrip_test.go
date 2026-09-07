// SPDX-License-Identifier: MPL-2.0
package wasm

import (
	"encoding/json"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
)

func TestCompatibilityControlRoundTrip(t *testing.T) {
	codecs := []struct {
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
		name      string
	}{
		{name: "json", marshal: json.Marshal, unmarshal: json.Unmarshal}, {name: "yaml", marshal: yaml.Marshal, unmarshal: yaml.Unmarshal},
	}
	for _, codec := range codecs {
		for _, validateFirst := range []bool{false, true} {
			for _, spelling := range []string{"limits", "options", "meta"} {
				t.Run(codec.name+"/"+spelling+map[bool]string{true: "/validated", false: "/raw"}[validateFirst], func(t *testing.T) {
					raw := map[string]any{"fs": "app:fs", "path": "/fn.wasm", "hash": "sha256:0", "method": "run", "source": "(module)"}
					limits := map[string]any{"max_execution_ms": 41}
					switch spelling {
					case "limits":
						raw["limits"] = limits
					case "options":
						raw["options"] = map[string]any{"limits": limits}
					case "meta":
						raw["meta"] = map[string]any{"options": map[string]any{"limits": limits}}
					}
					input, err := codec.marshal(raw)
					require.NoError(t, err)
					configs := []interface{ Validate() error }{&FunctionConfig{}, &WATFunctionConfig{}, &ProcessConfig{}}
					for _, cfg := range configs {
						require.NoError(t, codec.unmarshal(input, cfg))
						if validateFirst {
							require.NoError(t, cfg.Validate())
						}
						encoded, err := codec.marshal(cfg)
						require.NoError(t, err)
						require.NoError(t, codec.unmarshal(encoded, cfg))
						require.NoError(t, cfg.Validate())
						switch c := cfg.(type) {
						case *FunctionConfig:
							require.Equal(t, 41, c.Limits.MaxExecutionMS)
						case *WATFunctionConfig:
							require.Equal(t, 41, c.Limits.MaxExecutionMS)
						case *ProcessConfig:
							require.EqualValues(t, 41, c.Limits().MaxExecutionMS)
						}
					}
				})
			}
		}
	}
}

func TestCompatibilityProgrammaticControlsAndReplacement(t *testing.T) {
	for _, canonical := range []bool{false, true} {
		cfg := FunctionConfig{FS: "app:fs", Path: "/fn.wasm", Hash: "sha256:0", Method: "run"}
		limits := map[string]any{"max_execution_ms": 41}
		if canonical {
			cfg.OptionsConfig = map[string]any{"limits": limits}
		} else {
			cfg.RootLimits = limits
		}
		require.NoError(t, cfg.Validate())
		require.Equal(t, 41, cfg.Limits.MaxExecutionMS)
		opts := cfg.Options()
		opts.Limits.MaxExecutionMS = 55
		cfg.SetOptions(opts)
		require.NoError(t, cfg.Validate())
		require.Equal(t, 55, cfg.Limits.MaxExecutionMS)
	}
	var cfg FunctionConfig
	require.NoError(t, json.Unmarshal([]byte(`{"fs":"app:fs","path":"/fn.wasm","hash":"sha256:0","method":"run","limits":{"max_execution_ms":41},"meta":{"options":{"retry":3}}}`), &cfg))
	require.NoError(t, cfg.Validate())
	opts := cfg.Options()
	opts.Limits.MaxExecutionMS = 55
	cfg.SetOptions(opts)
	require.NoError(t, cfg.Validate())
	encoded, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, &cfg))
	require.NoError(t, cfg.Validate())
	require.Equal(t, 55, cfg.Limits.MaxExecutionMS)
	require.Contains(t, string(encoded), `"retry":3`)
}

func TestCompatibilityProgrammaticActorAndWATControls(t *testing.T) {
	for _, canonical := range []bool{false, true} {
		limits := map[string]any{"max_execution_ms": 41}
		actor := ProcessConfig{FS: "app:fs", Path: "/actor.wasm", Hash: "sha256:0", Method: "run"}
		wat := WATFunctionConfig{Source: "(module)", Method: "run"}
		if canonical {
			actor.OptionsConfig = map[string]any{"limits": limits}
			wat.OptionsConfig = map[string]any{"limits": limits}
		} else {
			actor.RootLimits = limits
			wat.RootLimits = limits
		}
		require.EqualValues(t, 41, actor.Options().Limits.MaxExecutionMS)
		require.Equal(t, 41, wat.Options().Limits.MaxExecutionMS)
		require.NoError(t, actor.Validate())
		require.NoError(t, wat.Validate())
		actorOpts := actor.Options()
		actorOpts.Limits.MaxExecutionMS = 55
		actor.SetOptions(actorOpts)
		watOpts := wat.Options()
		watOpts.Limits.MaxExecutionMS = 55
		wat.SetOptions(watOpts)
		require.NoError(t, actor.Validate())
		require.NoError(t, wat.Validate())
		require.EqualValues(t, 55, actor.Limits().MaxExecutionMS)
		require.Equal(t, 55, wat.Limits.MaxExecutionMS)
	}
}

func TestTypedLegacyOptionsRoundTripAndSetOptions(t *testing.T) {
	function := FunctionConfig{
		FS: "app:fs", Path: "/fn.wasm", Hash: "sha256:0", Method: "run",
		Meta: attrs.Bag{"options": FunctionOptions{Limits: LimitsConfig{MaxExecutionMS: 41}}},
	}
	require.NoError(t, function.Validate())
	functionOptions := function.Options()
	functionOptions.Limits.MaxExecutionMS = 55
	function.SetOptions(functionOptions)
	require.NoError(t, function.Validate())
	_, retained := function.Meta.Get("options")
	assert.False(t, retained, "SetOptions must remove typed legacy controls")
	encoded, err := json.Marshal(function)
	require.NoError(t, err)
	var functionRoundTrip FunctionConfig
	require.NoError(t, json.Unmarshal(encoded, &functionRoundTrip))
	require.NoError(t, functionRoundTrip.Validate())
	assert.Equal(t, 55, functionRoundTrip.Limits.MaxExecutionMS)

	wat := WATFunctionConfig{
		Source: "(module)", Method: "run",
		Meta: attrs.Bag{"options": &FunctionOptions{Limits: LimitsConfig{MaxExecutionMS: 41}}},
	}
	require.NoError(t, wat.Validate())
	watOptions := wat.Options()
	watOptions.Limits.MaxExecutionMS = 55
	wat.SetOptions(watOptions)
	require.NoError(t, wat.Validate())
	_, retained = wat.Meta.Get("options")
	assert.False(t, retained)
	encoded, err = json.Marshal(wat)
	require.NoError(t, err)
	var watRoundTrip WATFunctionConfig
	require.NoError(t, json.Unmarshal(encoded, &watRoundTrip))
	require.NoError(t, watRoundTrip.Validate())
	assert.Equal(t, 55, watRoundTrip.Limits.MaxExecutionMS)

	process := ProcessConfig{
		FS: "app:fs", Path: "/actor.wasm", Hash: "sha256:0", Method: "run",
		Meta: attrs.Bag{"options": ProcessOptions{Limits: ProcessLimitsConfig{MemoryBytes: 64 * 1024, MaxExecutionMS: 41}}},
	}
	require.NoError(t, process.Validate())
	processOptions := process.Options()
	processOptions.Limits.MaxExecutionMS = 55
	process.SetOptions(processOptions)
	require.NoError(t, process.Validate())
	_, retained = process.Meta.Get("options")
	assert.False(t, retained)
	encoded, err = json.Marshal(process)
	require.NoError(t, err)
	var processRoundTrip ProcessConfig
	require.NoError(t, json.Unmarshal(encoded, &processRoundTrip))
	require.NoError(t, processRoundTrip.Validate())
	assert.Equal(t, 55, processRoundTrip.Limits().MaxExecutionMS)
}

func TestTypedLegacyNilOptionPointersRemainAbsent(t *testing.T) {
	var functionOptions *FunctionOptions
	function := FunctionConfig{
		FS: "app:fs", Path: "/fn.wasm", Hash: "sha256:0", Method: "run",
		Meta: attrs.Bag{"options": functionOptions},
	}
	require.NoError(t, function.Validate())
	assert.Zero(t, function.Limits.MaxExecutionMS)

	var processOptions *ProcessOptions
	process := ProcessConfig{
		FS: "app:fs", Path: "/actor.wasm", Hash: "sha256:0", Method: "run",
		Meta: attrs.Bag{"options": processOptions},
	}
	require.NoError(t, process.Validate())
	assert.EqualValues(t, 0, process.Limits().MaxExecutionMS)
}

func TestTypedOptionsRejectInvalidValuesBeforeSerialization(t *testing.T) {
	newFunction := func() FunctionConfig {
		return FunctionConfig{FS: "app:fs", Path: "/fn.wasm", Hash: "sha256:0", Method: "run"}
	}
	for _, configure := range []func(*FunctionConfig){
		func(c *FunctionConfig) {
			c.Meta = attrs.Bag{"options": FunctionOptions{Limits: LimitsConfig{MaxExecutionMS: -1}}}
		},
		func(c *FunctionConfig) { c.OptionsConfig = FunctionOptions{Limits: LimitsConfig{MaxExecutionMS: -1}} },
		func(c *FunctionConfig) { c.SetOptions(FunctionOptions{Limits: LimitsConfig{MaxExecutionMS: -1}}) },
	} {
		cfg := newFunction()
		configure(&cfg)
		require.ErrorIs(t, cfg.Validate(), ErrInvalidExecutionLimit)
		_, err := json.Marshal(cfg)
		require.ErrorIs(t, err, ErrInvalidExecutionLimit)
	}

	newProcess := func() ProcessConfig {
		return ProcessConfig{FS: "app:fs", Path: "/actor.wasm", Hash: "sha256:0", Method: "run"}
	}
	invalid := ProcessOptions{Limits: ProcessLimitsConfig{MemoryBytes: -1}}
	for _, configure := range []func(*ProcessConfig){
		func(c *ProcessConfig) { c.Meta = attrs.Bag{"options": invalid} },
		func(c *ProcessConfig) { c.OptionsConfig = invalid },
		func(c *ProcessConfig) { c.SetOptions(invalid) },
	} {
		cfg := newProcess()
		configure(&cfg)
		require.ErrorIs(t, cfg.Validate(), ErrProcessMemoryBytesInvalid)
		_, err := json.Marshal(cfg)
		require.ErrorIs(t, err, ErrProcessMemoryBytesInvalid)
	}
}
