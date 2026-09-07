// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// Function configuration errors.
var (
	// ErrFunctionRootLimitsForbidden is retained for source compatibility.
	//
	// Deprecated: flat root limits are accepted with an admission warning.
	ErrFunctionRootLimitsForbidden = apierror.New(apierror.Invalid, "root-level limits not allowed for function; configure limits in meta.options.limits").WithRetryable(apierror.False)

	// ErrFunctionRootPoolForbidden is retained for source compatibility.
	//
	// Deprecated: root pool is accepted and remains the canonical pool path.
	ErrFunctionRootPoolForbidden = apierror.New(apierror.Invalid, "root-level pool not allowed for function; configure pool in meta.options.pool").WithRetryable(apierror.False)

	// ErrFunctionOptionsInvalidType is returned when the options value has an unsupported type.
	ErrFunctionOptionsInvalidType = apierror.New(apierror.Invalid, "meta.options must be an object").WithRetryable(apierror.False)

	// ErrFunctionLimitsInvalidType is returned when an authored limits control is not an object.
	ErrFunctionLimitsInvalidType = apierror.New(apierror.Invalid, "meta.options.limits must be an object").WithRetryable(apierror.False)

	// ErrFunctionPoolInvalidType is returned when an authored pool control is not an object.
	ErrFunctionPoolInvalidType = apierror.New(apierror.Invalid, "meta.options.pool must be an object").WithRetryable(apierror.False)

	// ErrFunctionPoolTypeInvalidType is returned when a pool type field is not a string.
	ErrFunctionPoolTypeInvalidType = apierror.New(apierror.Invalid, "meta.options.pool.type must be a string").WithRetryable(apierror.False)

	// ErrFunctionPoolWorkerClassInvalidType is returned when a pool worker class field is not a string.
	ErrFunctionPoolWorkerClassInvalidType = apierror.New(apierror.Invalid, "meta.options.pool.worker_class must be a string").WithRetryable(apierror.False)

	// ErrFunctionPoolWarmStartInvalidType is returned when a pool warm-start field is not a boolean.
	ErrFunctionPoolWarmStartInvalidType = apierror.New(apierror.Invalid, "meta.options.pool.warm_start must be a boolean").WithRetryable(apierror.False)

	// ErrFunctionUnknownField is the sentinel cause when an unknown field is encountered inside options.
	ErrFunctionUnknownField = ErrProcessUnknownField
)

// FunctionOptions contains the typed pool and resource controls for function entries.
type FunctionOptions struct {
	Pool   PoolConfig   `json:"pool,omitempty" yaml:"pool,omitempty"`
	Limits LimitsConfig `json:"limits,omitempty" yaml:"limits,omitempty"`
}

// EffectivePool returns the configured pool settings.
func (o FunctionOptions) EffectivePool() PoolConfig {
	return o.Pool
}

// EffectiveLimits returns the configured limits settings.
func (o FunctionOptions) EffectiveLimits() LimitsConfig {
	return o.Limits
}

// Options returns execution controls with defaults applied. Validate reports invalid input.
func (c *FunctionConfig) Options() FunctionOptions {
	if c.options != nil {
		return *c.options
	}
	fallback := FunctionOptions{Pool: c.Pool, Limits: c.Limits}
	opts, _, err := normalizeFunctionConfigOptions(FunctionWASM, c.OptionsConfig, c.hasOptions, c.RootPool, c.hasRootPool, c.RootLimits, c.hasRootLimits, c.Meta, fallback)
	if err != nil {
		return fallback
	} // Validate reports invalid authored controls.
	return opts
}

// PoolConfig returns the resolved pool configuration.
func (c *FunctionConfig) PoolConfig() PoolConfig {
	return c.Options().Pool
}

// LimitsConfig returns the resolved limits configuration.
func (c *FunctionConfig) LimitsConfig() LimitsConfig {
	return c.Options().Limits
}

// SetOptions programmatically sets execution options on the configuration and replaces authored control aliases without changing invocation defaults.
func (c *FunctionConfig) SetOptions(opts FunctionOptions) {
	copy := opts
	c.options = &copy
	c.Pool = opts.Pool
	c.Limits = opts.Limits
	c.OptionsConfig = opts
	c.hasOptions = true
	c.RootLimits, c.RootPool = nil, nil
	c.hasRootLimits, c.hasRootPool = false, false
	c.deprecated = nil
	c.Meta = cloneMetaWithoutControlOptions(c.Meta, FunctionWASM)
}

// Options returns execution controls with defaults applied. Validate reports invalid input.
func (c *WATFunctionConfig) Options() FunctionOptions {
	if c.options != nil {
		return *c.options
	}
	fallback := FunctionOptions{Pool: c.Pool, Limits: c.Limits}
	opts, _, err := normalizeFunctionConfigOptions(FunctionWAT, c.OptionsConfig, c.hasOptions, c.RootPool, c.hasRootPool, c.RootLimits, c.hasRootLimits, c.Meta, fallback)
	if err != nil {
		return fallback
	} // Validate reports invalid authored controls.
	return opts
}

// PoolConfig returns the resolved pool configuration.
func (c *WATFunctionConfig) PoolConfig() PoolConfig {
	return c.Options().Pool
}

// LimitsConfig returns the resolved limits configuration.
func (c *WATFunctionConfig) LimitsConfig() LimitsConfig {
	return c.Options().Limits
}

// SetOptions programmatically sets execution options on the configuration and replaces authored control aliases without changing invocation defaults.
func (c *WATFunctionConfig) SetOptions(opts FunctionOptions) {
	copy := opts
	c.options = &copy
	c.Pool = opts.Pool
	c.Limits = opts.Limits
	c.OptionsConfig = opts
	c.hasOptions = true
	c.RootLimits, c.RootPool = nil, nil
	c.hasRootLimits, c.hasRootPool = false, false
	c.deprecated = nil
	c.Meta = cloneMetaWithoutControlOptions(c.Meta, FunctionWAT)
}

func serializedFunctionRootOptions(opts FunctionOptions) map[string]any {
	all := serializeFunctionOptions(opts)
	delete(all, "pool")
	return all
}

func serializedFunctionRootPool(opts FunctionOptions) any {
	all := serializeFunctionOptions(opts)
	return all["pool"]
}

func functionOptionsInput(v any) any {
	switch typed := v.(type) {
	case FunctionOptions:
		return typed
	case *FunctionOptions:
		if typed == nil {
			return nil
		}
		return typed
	default:
		return v
	}
}

func normalizeFunctionConfigOptions(kind registry.Kind, rootOptions any, hasOptions bool, rootPool any, hasPool bool, rootLimits any, hasLimits bool, meta attrs.Bag, fallback FunctionOptions) (FunctionOptions, []DeprecatedOptionPath, error) {
	data := map[string]any{}
	if hasOptions || rootOptions != nil {
		data["options"] = functionOptionsInput(rootOptions)
	}
	if hasPool || rootPool != nil {
		data["pool"] = rootPool
	}
	if hasLimits || rootLimits != nil {
		data["limits"] = rootLimits
	}
	groups, deprecated, err := NormalizeEntryOptions(kind, data, meta)
	if err != nil {
		return FunctionOptions{}, nil, err
	}
	if len(groups) == 0 {
		validated, err := validateFunctionOptionsStruct(fallback)
		return validated, deprecated, err
	}
	parsed, err := parseAndValidateFunctionOptions(attrs.Bag{"options": groups})
	return parsed, deprecated, err
}

// DeprecatedOptionPaths returns accepted legacy paths without exposing the
// configuration's internal diagnostic slice.
func (c *FunctionConfig) DeprecatedOptionPaths() []DeprecatedOptionPath {
	return append([]DeprecatedOptionPath(nil), c.deprecated...)
}
func (c *WATFunctionConfig) DeprecatedOptionPaths() []DeprecatedOptionPath {
	return append([]DeprecatedOptionPath(nil), c.deprecated...)
}

func parseAndValidateFunctionOptions(meta attrs.Bag) (FunctionOptions, error) {
	var opts FunctionOptions
	if meta == nil {
		return opts, nil
	}

	optVal, exists := meta.Get("options")
	if !exists || optVal == nil {
		return opts, nil
	}

	switch v := optVal.(type) {
	case FunctionOptions:
		return validateFunctionOptionsStruct(v)
	case *FunctionOptions:
		if v == nil {
			return opts, nil
		}
		return validateFunctionOptionsStruct(*v)
	}

	var optMap map[string]any
	switch v := optVal.(type) {
	case attrs.Bag:
		optMap = map[string]any(v)
	case map[string]any:
		optMap = v
	default:
		return opts, ErrFunctionOptionsInvalidType
	}

	for k := range optMap {
		switch k {
		case "pool", "limits":
		default:
			return opts, newUnknownFieldError("meta.options", k)
		}
	}

	// Pool
	if poolVal, ok := optMap["pool"]; ok && poolVal != nil {
		var poolMap map[string]any
		switch v := poolVal.(type) {
		case attrs.Bag:
			poolMap = map[string]any(v)
		case map[string]any:
			poolMap = v
		default:
			return opts, ErrFunctionPoolInvalidType
		}

		for k := range poolMap {
			switch k {
			case "type", "worker_class", "size", "workers", "buffer", "warm_start", "max_size":
			default:
				return opts, newUnknownFieldError("meta.options.pool", k)
			}
		}

		if v, ok := poolMap["type"]; ok && v != nil {
			s, ok := v.(string)
			if !ok {
				return opts, ErrFunctionPoolTypeInvalidType
			}
			opts.Pool.Type = s
		}

		if v, ok := poolMap["worker_class"]; ok && v != nil {
			s, ok := v.(string)
			if !ok {
				return opts, ErrFunctionPoolWorkerClassInvalidType
			}
			opts.Pool.WorkerClass = s
		}

		if v, ok := poolMap["size"]; ok && v != nil {
			val, err := asExactInt(v, "meta.options.pool.size")
			if err != nil {
				return opts, err
			}
			opts.Pool.Size = val
		}

		if v, ok := poolMap["workers"]; ok && v != nil {
			val, err := asExactInt(v, "meta.options.pool.workers")
			if err != nil {
				return opts, err
			}
			opts.Pool.Workers = val
		}

		if v, ok := poolMap["buffer"]; ok && v != nil {
			val, err := asExactInt(v, "meta.options.pool.buffer")
			if err != nil {
				return opts, err
			}
			opts.Pool.Buffer = val
		}

		if v, ok := poolMap["warm_start"]; ok && v != nil {
			b, ok := v.(bool)
			if !ok {
				return opts, ErrFunctionPoolWarmStartInvalidType
			}
			opts.Pool.WarmStart = b
		}

		if v, ok := poolMap["max_size"]; ok && v != nil {
			val, err := asExactInt(v, "meta.options.pool.max_size")
			if err != nil {
				return opts, err
			}
			opts.Pool.MaxSize = val
		}

		if err := validatePool(opts.Pool); err != nil {
			return opts, err
		}
	}

	// Limits
	if limVal, ok := optMap["limits"]; ok && limVal != nil {
		var limMap map[string]any
		switch v := limVal.(type) {
		case attrs.Bag:
			limMap = map[string]any(v)
		case map[string]any:
			limMap = v
		default:
			return opts, ErrFunctionLimitsInvalidType
		}

		for k := range limMap {
			switch k {
			case "max_execution_ms", "max_open_sockets", "socket_timeout_ms", "max_retained_memory_bytes", "retained_memory_check_interval":
			default:
				return opts, newUnknownFieldError("meta.options.limits", k)
			}
		}

		if v, ok := limMap["max_execution_ms"]; ok && v != nil {
			ms, err := asExactInt(v, "meta.options.limits.max_execution_ms")
			if err != nil {
				return opts, err
			}
			if ms < 0 {
				return opts, ErrInvalidExecutionLimit
			}
			opts.Limits.MaxExecutionMS = ms
		}

		if v, ok := limMap["max_open_sockets"]; ok && v != nil {
			s, err := asExactInt(v, "meta.options.limits.max_open_sockets")
			if err != nil {
				return opts, err
			}
			if s < 0 {
				return opts, ErrInvalidMaxOpenSockets
			}
			opts.Limits.MaxOpenSockets = s
		}

		if v, ok := limMap["socket_timeout_ms"]; ok && v != nil {
			t, err := asExactInt(v, "meta.options.limits.socket_timeout_ms")
			if err != nil {
				return opts, err
			}
			if t < 0 {
				return opts, ErrInvalidSocketTimeout
			}
			opts.Limits.SocketTimeoutMS = t
		}

		if v, ok := limMap["max_retained_memory_bytes"]; ok && v != nil {
			mb, err := asExactInt64(v, "meta.options.limits.max_retained_memory_bytes")
			if err != nil {
				return opts, err
			}
			if mb < 0 {
				return opts, ErrInvalidRetainedMemoryLimit
			}
			opts.Limits.SetMaxRetainedMemoryBytes(mb)
		}

		if v, ok := limMap["retained_memory_check_interval"]; ok && v != nil {
			ci, err := asExactInt(v, "meta.options.limits.retained_memory_check_interval")
			if err != nil {
				return opts, err
			}
			if ci < 0 {
				return opts, ErrInvalidRetainedMemoryCheckInterval
			}
			opts.Limits.RetainedMemoryCheckInterval = ci
		}

		if err := validateLimits(opts.Limits); err != nil {
			return opts, err
		}
	}

	return opts, nil
}

func validateFunctionOptionsStruct(opts FunctionOptions) (FunctionOptions, error) {
	if err := validatePool(opts.Pool); err != nil {
		return opts, err
	}
	if err := validateLimits(opts.Limits); err != nil {
		return opts, err
	}
	return opts, nil
}

func serializeFunctionOptions(opts FunctionOptions) map[string]any {
	optMap := map[string]any{}
	poolMap := map[string]any{}
	if opts.Pool.Type != "" {
		poolMap["type"] = opts.Pool.Type
	}
	if opts.Pool.WorkerClass != "" {
		poolMap["worker_class"] = opts.Pool.WorkerClass
	}
	if opts.Pool.Size > 0 {
		poolMap["size"] = opts.Pool.Size
	}
	if opts.Pool.Workers > 0 {
		poolMap["workers"] = opts.Pool.Workers
	}
	if opts.Pool.Buffer > 0 {
		poolMap["buffer"] = opts.Pool.Buffer
	}
	if opts.Pool.WarmStart {
		poolMap["warm_start"] = opts.Pool.WarmStart
	}
	if opts.Pool.MaxSize > 0 {
		poolMap["max_size"] = opts.Pool.MaxSize
	}
	if len(poolMap) > 0 {
		optMap["pool"] = poolMap
	}

	limMap := map[string]any{}
	if opts.Limits.MaxExecutionMS > 0 {
		limMap["max_execution_ms"] = opts.Limits.MaxExecutionMS
	}
	if opts.Limits.MaxOpenSockets > 0 {
		limMap["max_open_sockets"] = opts.Limits.MaxOpenSockets
	}
	if opts.Limits.SocketTimeoutMS > 0 {
		limMap["socket_timeout_ms"] = opts.Limits.SocketTimeoutMS
	}
	if opts.Limits.HasMaxRetainedMemoryBytes() {
		limMap["max_retained_memory_bytes"] = opts.Limits.MaxRetainedMemoryBytes
	}
	if opts.Limits.RetainedMemoryCheckInterval > 0 {
		limMap["retained_memory_check_interval"] = opts.Limits.RetainedMemoryCheckInterval
	}
	if len(limMap) > 0 {
		optMap["limits"] = limMap
	}
	return optMap
}
