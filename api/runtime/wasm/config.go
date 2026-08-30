// SPDX-License-Identifier: MPL-2.0

// Package wasm provides WASM runtime integration.
package wasm

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

type (
	// PoolConfig defines settings for a pool of WASM executors.
	PoolConfig struct {
		Type        string `json:"type"`         // Pool type: static, lazy, inline, adaptive
		WorkerClass string `json:"worker_class"` // Optional scheduler worker class; a named class runs this pool on dedicated OS-thread-pinned workers (mirrors v2 scheduler worker classes)
		Size        int    `json:"size"`         // Total pool size for non-flex pools
		Workers     int    `json:"workers"`      // Number of worker threads
		Buffer      int    `json:"buffer"`       // Queue buffer size (default: workers * 64)

		// Elastic pool specifics.
		WarmStart bool `json:"warm_start"` // Pre-instantiate workers where applicable
		MaxSize   int  `json:"max_size"`   // Maximum workers for elastic pools
	}

	// LimitsConfig defines execution and warm-instance lifecycle limits for a
	// WASM function.
	LimitsConfig struct {
		MaxExecutionMS  int `json:"max_execution_ms,omitempty"`
		MaxOpenSockets  int `json:"max_open_sockets,omitempty"`
		SocketTimeoutMS int `json:"socket_timeout_ms,omitempty"`

		// MaxRetainedMemoryBytes is a post-call per-worker recycling trigger.
		// An omitted value uses DefaultMaxRetainedMemoryBytes. An explicit 0
		// disables retained-memory recycling.
		MaxRetainedMemoryBytes int64 `json:"max_retained_memory_bytes,omitempty"`

		// RetainedMemoryCheckInterval optionally amortizes post-call memory
		// inspection. When omitted, the built-in limit uses
		// DefaultRetainedMemoryCheckInterval and an explicit limit is checked after
		// every call.
		RetainedMemoryCheckInterval int `json:"retained_memory_check_interval,omitempty"`

		maxRetainedMemoryBytesSet bool
	}

	// WASIEnvVarConfig maps an env registry variable ID to a guest env var name.
	WASIEnvVarConfig struct {
		ID       registry.ID `json:"id"`
		Name     string      `json:"name"`
		Required bool        `json:"required,omitempty"`
	}

	// WASIMountConfig maps a runtime filesystem capability to a guest path.
	WASIMountConfig struct {
		FS       registry.ID `json:"fs"`
		Guest    string      `json:"guest"`
		ReadOnly bool        `json:"read_only,omitempty"`
	}

	// WASIConfig contains runtime-managed WASI input mapping.
	// env and mounts are explicit, capability-based mappings.
	WASIConfig struct {
		Args   []string           `json:"args,omitempty"`
		Cwd    string             `json:"cwd,omitempty"`
		Env    []WASIEnvVarConfig `json:"env,omitempty"`
		Mounts []WASIMountConfig  `json:"mounts,omitempty"`
	}

	// WATFunctionConfig defines configuration for inline WAT function entries.
	WATFunctionConfig struct {
		Meta      attrs.Bag     `json:"meta,omitempty"`
		Source    string        `json:"source" resolve:"-"`
		Method    string        `json:"method"`
		Transport string        `json:"transport,omitempty"`
		WIT       string        `json:"wit,omitempty"`
		WASI      WASIConfig    `json:"wasi,omitempty"`
		Imports   []registry.ID `json:"imports,omitempty"`
		Pool      PoolConfig    `json:"pool,omitempty"`
		Limits    LimitsConfig  `json:"limits,omitempty"`
	}

	// FunctionConfig defines configuration for precompiled WASM function entries.
	FunctionConfig struct {
		Meta      attrs.Bag     `json:"meta,omitempty"`
		FS        string        `json:"fs"`
		Path      string        `json:"path"`
		Hash      string        `json:"hash"`
		Method    string        `json:"method"`
		Transport string        `json:"transport,omitempty"`
		WIT       string        `json:"wit,omitempty"`
		WASI      WASIConfig    `json:"wasi,omitempty"`
		Imports   []registry.ID `json:"imports,omitempty"`
		Pool      PoolConfig    `json:"pool,omitempty"`
		Limits    LimitsConfig  `json:"limits,omitempty"`
	}
)

type limitsConfigJSON struct {
	MaxRetainedMemoryBytes      *int64 `json:"max_retained_memory_bytes,omitempty" yaml:"max_retained_memory_bytes,omitempty"`
	MaxExecutionMS              int    `json:"max_execution_ms,omitempty" yaml:"max_execution_ms,omitempty"`
	MaxOpenSockets              int    `json:"max_open_sockets,omitempty" yaml:"max_open_sockets,omitempty"`
	SocketTimeoutMS             int    `json:"socket_timeout_ms,omitempty" yaml:"socket_timeout_ms,omitempty"`
	RetainedMemoryCheckInterval int    `json:"retained_memory_check_interval,omitempty" yaml:"retained_memory_check_interval,omitempty"`
}

func (c *LimitsConfig) UnmarshalJSON(data []byte) error {
	var decoded limitsConfigJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	c.applyDecoded(decoded)
	return nil
}

func (c *LimitsConfig) UnmarshalYAML(unmarshal func(any) error) error {
	var decoded limitsConfigJSON
	if err := unmarshal(&decoded); err != nil {
		return err
	}
	c.applyDecoded(decoded)
	return nil
}

func (c *LimitsConfig) applyDecoded(decoded limitsConfigJSON) {
	*c = LimitsConfig{
		MaxExecutionMS:              decoded.MaxExecutionMS,
		MaxOpenSockets:              decoded.MaxOpenSockets,
		SocketTimeoutMS:             decoded.SocketTimeoutMS,
		RetainedMemoryCheckInterval: decoded.RetainedMemoryCheckInterval,
	}
	if decoded.MaxRetainedMemoryBytes != nil {
		c.SetMaxRetainedMemoryBytes(*decoded.MaxRetainedMemoryBytes)
	}
}

func (c LimitsConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.encoded())
}

func (c LimitsConfig) MarshalYAML() (any, error) {
	return c.encoded(), nil
}

func (c LimitsConfig) encoded() limitsConfigJSON {
	encoded := limitsConfigJSON{
		MaxExecutionMS:              c.MaxExecutionMS,
		MaxOpenSockets:              c.MaxOpenSockets,
		SocketTimeoutMS:             c.SocketTimeoutMS,
		RetainedMemoryCheckInterval: c.RetainedMemoryCheckInterval,
	}
	if c.HasMaxRetainedMemoryBytes() {
		value := c.MaxRetainedMemoryBytes
		encoded.MaxRetainedMemoryBytes = &value
	}
	return encoded
}

func (c LimitsConfig) HasMaxRetainedMemoryBytes() bool {
	return c.maxRetainedMemoryBytesSet || c.MaxRetainedMemoryBytes != 0
}

func (c *LimitsConfig) SetMaxRetainedMemoryBytes(value int64) {
	c.MaxRetainedMemoryBytes = value
	c.maxRetainedMemoryBytesSet = true
}

func (c LimitsConfig) EffectiveMaxRetainedMemoryBytes() int64 {
	if c.HasMaxRetainedMemoryBytes() {
		return c.MaxRetainedMemoryBytes
	}
	return DefaultMaxRetainedMemoryBytes
}

func (c LimitsConfig) Validate() error {
	return validateLimits(c)
}

func (c LimitsConfig) EffectiveRetainedMemoryCheckInterval() int {
	if c.RetainedMemoryCheckInterval > 0 {
		return c.RetainedMemoryCheckInterval
	}
	return DefaultRetainedMemoryCheckInterval
}

func (c LimitsConfig) EffectiveMaxOpenSockets() int {
	if c.MaxOpenSockets > 0 {
		return c.MaxOpenSockets
	}
	return DefaultMaxOpenSockets
}

func (c LimitsConfig) EffectiveSocketTimeoutMS() int {
	if c.SocketTimeoutMS > 0 {
		return c.SocketTimeoutMS
	}
	return DefaultSocketTimeoutMS
}

// EffectiveTransport returns the transport, defaulting to payload.
func (c *WATFunctionConfig) EffectiveTransport() string {
	if c.Transport == "" {
		return TransportTypePayload
	}
	return c.Transport
}

// Validate checks if the WATFunctionConfig has required fields and valid values.
func (c *WATFunctionConfig) Validate() error {
	if c.Source == "" {
		return ErrSourceRequired
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	if err := validateImports(c.Imports); err != nil {
		return err
	}
	if err := validateTransport(c.Transport); err != nil {
		return err
	}
	if err := validatePool(c.Pool); err != nil {
		return err
	}
	if err := validateLimits(c.Limits); err != nil {
		return err
	}
	if err := validateWASI(c.WASI); err != nil {
		return err
	}
	return nil
}

// EffectiveTransport returns the transport, defaulting to payload.
func (c *FunctionConfig) EffectiveTransport() string {
	if c.Transport == "" {
		return TransportTypePayload
	}
	return c.Transport
}

// Validate checks if the WASMFunctionConfig has required fields and valid values.
func (c *FunctionConfig) Validate() error {
	if c.FS == "" {
		return ErrFSRequired
	}
	if c.Path == "" {
		return ErrPathRequired
	}
	if c.Hash == "" {
		return ErrHashRequired
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	if err := validateImports(c.Imports); err != nil {
		return err
	}
	if err := validateTransport(c.Transport); err != nil {
		return err
	}
	if err := validatePool(c.Pool); err != nil {
		return err
	}
	if err := validateLimits(c.Limits); err != nil {
		return err
	}
	if err := validateWASI(c.WASI); err != nil {
		return err
	}
	return nil
}

func validateImports(imports []registry.ID) error {
	for _, id := range imports {
		if id.Name == "" {
			return ErrEmptyImportName
		}
	}
	return nil
}

func validateTransport(transport string) error {
	switch transport {
	case "", TransportTypePayload, TransportTypeWASIHTTP:
		return nil
	default:
		return ErrInvalidTransportType
	}
}

func validatePool(pool PoolConfig) error {
	if pool.Size < 0 || pool.Workers < 0 || pool.Buffer < 0 || pool.MaxSize < 0 {
		return ErrInvalidPoolConfig
	}

	if pool.Type != "" {
		switch pool.Type {
		case PoolTypeLazy, PoolTypeStatic, PoolTypeInline, PoolTypeAdaptive:
		default:
			return ErrInvalidPoolType
		}
	}

	if pool.WorkerClass != "" {
		// A worker class selects a dedicated, OS-thread-pinned pool that derives
		// its own worker/buffer defaults, so the legacy size semantics below do
		// not apply. The class name is resolved by the scheduler at boot.
		return nil
	}

	// Legacy-compatible validation semantics from lua runtime:
	// - workers=0,size=0 is flex/lazy style
	// - workers>0 requires size>0
	isFlexPool := pool.Workers == 0 && (pool.Size == 0 || pool.MaxSize > 0)
	if !isFlexPool && pool.Size <= 0 {
		return ErrInvalidPoolSize
	}
	if pool.Workers > 0 && pool.Size <= 0 {
		return ErrInvalidWorkerPoolSize
	}

	return nil
}

func validateLimits(limits LimitsConfig) error {
	if limits.MaxExecutionMS < 0 {
		return ErrInvalidExecutionLimit
	}
	if limits.MaxRetainedMemoryBytes < 0 {
		return ErrInvalidRetainedMemoryLimit
	}
	if limits.RetainedMemoryCheckInterval < 0 {
		return ErrInvalidRetainedMemoryCheckInterval
	}
	if limits.MaxOpenSockets < 0 {
		return ErrInvalidMaxOpenSockets
	}
	if limits.SocketTimeoutMS < 0 {
		return ErrInvalidSocketTimeout
	}
	return nil
}

func validateWASI(cfg WASIConfig) error {
	if cfg.Cwd != "" && !strings.HasPrefix(cfg.Cwd, "/") {
		return ErrWASICwdMustBeAbsolute
	}

	seenEnv := make(map[string]struct{}, len(cfg.Env))
	for _, item := range cfg.Env {
		if item.ID.Name == "" {
			return ErrWASIEnvIDRequired
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return ErrWASIEnvNameRequired
		}
		if _, exists := seenEnv[name]; exists {
			return ErrWASIEnvNameDuplicate
		}
		seenEnv[name] = struct{}{}
	}

	seenMounts := make(map[string]struct{}, len(cfg.Mounts))
	for _, item := range cfg.Mounts {
		if item.FS.Name == "" {
			return ErrWASIMountFSRequired
		}
		guest := strings.TrimSpace(item.Guest)
		if guest == "" {
			return ErrWASIMountGuestRequired
		}
		if !strings.HasPrefix(guest, "/") {
			return ErrWASIMountGuestMustBeAbsolute
		}
		guest = path.Clean(guest)
		if guest == "." {
			return ErrWASIMountGuestRequired
		}
		if _, exists := seenMounts[guest]; exists {
			return ErrWASIMountGuestDuplicate
		}
		seenMounts[guest] = struct{}{}
	}

	return nil
}
