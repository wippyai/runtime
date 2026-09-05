// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

const (
	// DefaultProcessMemoryBytes is the default linear memory ceiling for a persistent WASM actor (64 MiB).
	DefaultProcessMemoryBytes int64 = 64 * 1024 * 1024

	// MinProcessMemoryBytesMultiple is the required page alignment for memory_bytes (64 KiB WASM page size).
	MinProcessMemoryBytesMultiple int64 = 64 * 1024

	// MaxProcessMemoryBytes is the maximum allowed linear memory ceiling for a WASM actor (4 GiB).
	MaxProcessMemoryBytes int64 = 4 * 1024 * 1024 * 1024

	// DefaultProcessMailboxCapacity is the default maximum number of queued messages (128).
	DefaultProcessMailboxCapacity int = 128

	// DefaultProcessMailboxBytes is the default maximum aggregate memory for queued messages (8 MiB).
	DefaultProcessMailboxBytes int64 = 8 * 1024 * 1024

	// DefaultProcessMailboxMsgBytes is the default maximum size of an individual message (1 MiB).
	DefaultProcessMailboxMsgBytes int64 = 1 * 1024 * 1024

	// DefaultProcessWorkerClass is the default dedicated worker class for CPU-isolated actor execution ("wasm").
	DefaultProcessWorkerClass = WorkerClassWASM
)

// Process configuration errors.
var (
	// ErrProcessRootLimitsForbidden is returned when root-level limits are provided on a process.wasm entry.
	ErrProcessRootLimitsForbidden = apierror.New(apierror.Invalid, "root-level limits not allowed for process.wasm; configure limits in meta.options").WithRetryable(apierror.False)

	// ErrProcessRootPoolForbidden is returned when root-level pool is provided on a process.wasm entry.
	ErrProcessRootPoolForbidden = apierror.New(apierror.Invalid, "root-level pool not allowed for process.wasm; pooling is meaningless for a persistent actor").WithRetryable(apierror.False)

	// ErrProcessOptionsInvalidType is returned when meta.options is not an object.
	ErrProcessOptionsInvalidType = apierror.New(apierror.Invalid, "meta.options must be an object").WithRetryable(apierror.False)

	// ErrProcessLimitsInvalidType is returned when meta.options.limits is not an object.
	ErrProcessLimitsInvalidType = apierror.New(apierror.Invalid, "meta.options.limits must be an object").WithRetryable(apierror.False)

	// ErrProcessMailboxInvalidType is returned when meta.options.mailbox is not an object.
	ErrProcessMailboxInvalidType = apierror.New(apierror.Invalid, "meta.options.mailbox must be an object").WithRetryable(apierror.False)

	// ErrProcessWorkerClassInvalidType is returned when meta.options.worker_class is not a string.
	ErrProcessWorkerClassInvalidType = apierror.New(apierror.Invalid, "meta.options.worker_class must be a string").WithRetryable(apierror.False)

	// ErrProcessMemoryBytesInvalid is returned when memory_bytes is zero or negative.
	ErrProcessMemoryBytesInvalid = apierror.New(apierror.Invalid, "limits.memory_bytes must be positive").WithRetryable(apierror.False)

	// ErrProcessMemoryBytesExceeded is returned when memory_bytes exceeds 4 GiB.
	ErrProcessMemoryBytesExceeded = apierror.New(apierror.Invalid, "limits.memory_bytes cannot exceed 4GiB").WithRetryable(apierror.False)

	// ErrProcessMemoryBytesAlignment is returned when memory_bytes is not an exact multiple of 64 KiB.
	ErrProcessMemoryBytesAlignment = apierror.New(apierror.Invalid, "limits.memory_bytes must be a multiple of 64KiB").WithRetryable(apierror.False)

	// ErrProcessMailboxCapacityInvalid is returned when mailbox capacity is zero or negative.
	ErrProcessMailboxCapacityInvalid = apierror.New(apierror.Invalid, "mailbox.capacity must be greater than 0").WithRetryable(apierror.False)

	// ErrProcessMailboxBytesInvalid is returned when mailbox bytes budget is zero or negative.
	ErrProcessMailboxBytesInvalid = apierror.New(apierror.Invalid, "mailbox.bytes must be greater than 0").WithRetryable(apierror.False)

	// ErrProcessMailboxMessageBytesInvalid is returned when mailbox message_bytes is zero or negative.
	ErrProcessMailboxMessageBytesInvalid = apierror.New(apierror.Invalid, "mailbox.message_bytes must be greater than 0").WithRetryable(apierror.False)

	// ErrProcessMailboxBudgetInconsistent is returned when message_bytes exceeds total mailbox bytes or capacity exceeds bytes/256.
	ErrProcessMailboxBudgetInconsistent = apierror.New(apierror.Invalid, "mailbox.message_bytes cannot exceed mailbox.bytes and mailbox.capacity cannot exceed mailbox.bytes/256").WithRetryable(apierror.False)

	// ErrProcessUnknownField is the sentinel cause when an unknown field is encountered inside controls.
	ErrProcessUnknownField = apierror.New(apierror.Invalid, "unknown field in controls").WithRetryable(apierror.False)
)

func newUnknownFieldError(path, field string) error {
	return apierror.New(apierror.Invalid, fmt.Sprintf("unknown field %q in %s", field, path)).
		WithRetryable(apierror.False).
		WithCause(ErrProcessUnknownField)
}

type (
	// ProcessLimitsConfig defines execution and resource limits for a persistent WASM actor.
	// Configured within meta.options.limits.
	ProcessLimitsConfig struct {
		// MemoryBytes defines the actor linear memory ceiling.
		// Defaults to 64 MiB (67108864). Must be a positive multiple of 64 KiB <= 4 GiB.
		MemoryBytes int64 `json:"memory_bytes,omitempty" yaml:"memory_bytes,omitempty"`

		// MaxExecutionMS defines the actor lifetime limit in milliseconds if nonzero.
		// A value of 0 indicates indefinite lifetime. Must not be negative.
		MaxExecutionMS int `json:"max_execution_ms,omitempty" yaml:"max_execution_ms,omitempty"`

		// MaxOpenSockets defines maximum concurrent open outbound/inbound sockets.
		// Defaults to DefaultMaxOpenSockets (16). Must not be negative.
		MaxOpenSockets int `json:"max_open_sockets,omitempty" yaml:"max_open_sockets,omitempty"`

		// SocketTimeoutMS defines socket I/O timeout in milliseconds.
		// Defaults to DefaultSocketTimeoutMS (30000). Must not be negative.
		SocketTimeoutMS int `json:"socket_timeout_ms,omitempty" yaml:"socket_timeout_ms,omitempty"`
	}

	// ProcessMailboxConfig defines message queue and buffering controls for a persistent WASM actor.
	// Configured within meta.options.mailbox.
	ProcessMailboxConfig struct {
		// Capacity defines maximum number of unread messages queued in the actor mailbox.
		// Defaults to 128. Must be > 0.
		Capacity int `json:"capacity,omitempty" yaml:"capacity,omitempty"`

		// Bytes defines total memory ceiling across all queued mailbox messages.
		// Defaults to 8 MiB (8388608). Must be > 0 and >= MessageBytes.
		Bytes int64 `json:"bytes,omitempty" yaml:"bytes,omitempty"`

		// MessageBytes defines maximum allowed size of any individual message.
		// Defaults to 1 MiB (1048576). Must be > 0 and <= Bytes.
		MessageBytes int64 `json:"message_bytes,omitempty" yaml:"message_bytes,omitempty"`
	}

	// ProcessOptions encapsulates execution controls defined inside meta.options.
	ProcessOptions struct {
		WorkerClass string               `json:"worker_class,omitempty" yaml:"worker_class,omitempty"`
		Limits      ProcessLimitsConfig  `json:"limits,omitempty" yaml:"limits,omitempty"`
		Mailbox     ProcessMailboxConfig `json:"mailbox,omitempty" yaml:"mailbox,omitempty"`
	}

	// ProcessConfig defines configuration for precompiled WASM process/actor entries (process.wasm).
	// It describes the static code configuration (fs/path/hash/method/imports/wit/wasi) and
	// extracts actor execution controls from meta.options.
	//
	// Root-level limits and pool are explicitly forbidden; persistent actors do not support
	// function pooling or root-level limits.
	ProcessConfig struct {
		Meta     attrs.Bag        `json:"meta,omitempty" yaml:"meta,omitempty"`
		Security *security.Config `json:"security,omitempty" yaml:"security,omitempty"`

		// RootLimits and RootPool capture root-level limits and pool to detect and reject invalid configuration.
		RootLimits any `json:"limits,omitempty" yaml:"limits,omitempty"`
		RootPool   any `json:"pool,omitempty" yaml:"pool,omitempty"`

		options       *ProcessOptions
		FS            string        `json:"fs" yaml:"fs"`
		Path          string        `json:"path" yaml:"path"`
		Hash          string        `json:"hash" yaml:"hash"`
		Method        string        `json:"method" yaml:"method"`
		Transport     string        `json:"transport,omitempty" yaml:"transport,omitempty"`
		WIT           string        `json:"wit,omitempty" yaml:"wit,omitempty"`
		WASI          WASIConfig    `json:"wasi,omitempty" yaml:"wasi,omitempty"`
		Imports       []registry.ID `json:"imports,omitempty" yaml:"imports,omitempty"`
		hasRootLimits bool
		hasRootPool   bool
	}
)

type processConfigJSON struct {
	Meta      attrs.Bag        `json:"meta,omitempty" yaml:"meta,omitempty"`
	Security  *security.Config `json:"security,omitempty" yaml:"security,omitempty"`
	Limits    any              `json:"limits,omitempty" yaml:"limits,omitempty"`
	Pool      any              `json:"pool,omitempty" yaml:"pool,omitempty"`
	FS        string           `json:"fs" yaml:"fs"`
	Path      string           `json:"path" yaml:"path"`
	Hash      string           `json:"hash" yaml:"hash"`
	Method    string           `json:"method" yaml:"method"`
	Transport string           `json:"transport,omitempty" yaml:"transport,omitempty"`
	WIT       string           `json:"wit,omitempty" yaml:"wit,omitempty"`
	WASI      WASIConfig       `json:"wasi,omitempty" yaml:"wasi,omitempty"`
	Imports   []registry.ID    `json:"imports,omitempty" yaml:"imports,omitempty"`
}

// UnmarshalJSON deserializes ProcessConfig and flags any presence of root-level limits or pool.
func (c *ProcessConfig) UnmarshalJSON(data []byte) error {
	var decoded processConfigJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	var hasLimits, hasPool bool
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err == nil {
		if _, exists := rawMap["limits"]; exists {
			hasLimits = true
		}
		if _, exists := rawMap["pool"]; exists {
			hasPool = true
		}
	}

	*c = ProcessConfig{
		Meta:          decoded.Meta,
		Security:      decoded.Security,
		FS:            decoded.FS,
		Path:          decoded.Path,
		Hash:          decoded.Hash,
		Method:        decoded.Method,
		Transport:     decoded.Transport,
		WIT:           decoded.WIT,
		WASI:          decoded.WASI,
		Imports:       decoded.Imports,
		RootLimits:    decoded.Limits,
		RootPool:      decoded.Pool,
		hasRootLimits: hasLimits,
		hasRootPool:   hasPool,
		options:       nil,
	}

	return nil
}

// UnmarshalYAML deserializes ProcessConfig and flags any presence of root-level limits or pool.
func (c *ProcessConfig) UnmarshalYAML(unmarshal func(any) error) error {
	var decoded processConfigJSON
	if err := unmarshal(&decoded); err != nil {
		return err
	}

	var hasLimits, hasPool bool
	var rawMap map[string]any
	if err := unmarshal(&rawMap); err == nil {
		if _, exists := rawMap["limits"]; exists {
			hasLimits = true
		}
		if _, exists := rawMap["pool"]; exists {
			hasPool = true
		}
	}

	*c = ProcessConfig{
		Meta:          decoded.Meta,
		Security:      decoded.Security,
		FS:            decoded.FS,
		Path:          decoded.Path,
		Hash:          decoded.Hash,
		Method:        decoded.Method,
		Transport:     decoded.Transport,
		WIT:           decoded.WIT,
		WASI:          decoded.WASI,
		Imports:       decoded.Imports,
		RootLimits:    decoded.Limits,
		RootPool:      decoded.Pool,
		hasRootLimits: hasLimits,
		hasRootPool:   hasPool,
		options:       nil,
	}

	return nil
}

// MarshalJSON serializes the ProcessConfig, excluding any root limits or pool fields.
func (c ProcessConfig) MarshalJSON() ([]byte, error) {
	return json.Marshal(processConfigJSON{
		Meta:      c.Meta,
		Security:  c.Security,
		FS:        c.FS,
		Path:      c.Path,
		Hash:      c.Hash,
		Method:    c.Method,
		Transport: c.Transport,
		WIT:       c.WIT,
		WASI:      c.WASI,
		Imports:   c.Imports,
	})
}

// MarshalYAML serializes the ProcessConfig, excluding any root limits or pool fields.
func (c ProcessConfig) MarshalYAML() (any, error) {
	return processConfigJSON{
		Meta:      c.Meta,
		Security:  c.Security,
		FS:        c.FS,
		Path:      c.Path,
		Hash:      c.Hash,
		Method:    c.Method,
		Transport: c.Transport,
		WIT:       c.WIT,
		WASI:      c.WASI,
		Imports:   c.Imports,
	}, nil
}

// EffectiveMemoryBytes returns configured memory bytes or DefaultProcessMemoryBytes.
func (l ProcessLimitsConfig) EffectiveMemoryBytes() int64 {
	if l.MemoryBytes > 0 {
		return l.MemoryBytes
	}
	return DefaultProcessMemoryBytes
}

// EffectiveMaxExecutionMS returns maximum execution time in ms (0 = indefinite).
func (l ProcessLimitsConfig) EffectiveMaxExecutionMS() int {
	return l.MaxExecutionMS
}

// EffectiveMaxOpenSockets returns configured socket limit or DefaultMaxOpenSockets.
func (l ProcessLimitsConfig) EffectiveMaxOpenSockets() int {
	if l.MaxOpenSockets > 0 {
		return l.MaxOpenSockets
	}
	return DefaultMaxOpenSockets
}

// EffectiveSocketTimeoutMS returns configured socket timeout or DefaultSocketTimeoutMS.
func (l ProcessLimitsConfig) EffectiveSocketTimeoutMS() int {
	if l.SocketTimeoutMS > 0 {
		return l.SocketTimeoutMS
	}
	return DefaultSocketTimeoutMS
}

// EffectiveCapacity returns configured mailbox capacity or DefaultProcessMailboxCapacity.
func (m ProcessMailboxConfig) EffectiveCapacity() int {
	if m.Capacity > 0 {
		return m.Capacity
	}
	return DefaultProcessMailboxCapacity
}

// EffectiveBytes returns configured mailbox byte limit or DefaultProcessMailboxBytes.
func (m ProcessMailboxConfig) EffectiveBytes() int64 {
	if m.Bytes > 0 {
		return m.Bytes
	}
	return DefaultProcessMailboxBytes
}

// EffectiveMessageBytes returns configured max message bytes or DefaultProcessMailboxMsgBytes.
func (m ProcessMailboxConfig) EffectiveMessageBytes() int64 {
	if m.MessageBytes > 0 {
		return m.MessageBytes
	}
	return DefaultProcessMailboxMsgBytes
}

// EffectiveWorkerClass returns configured worker class or DefaultProcessWorkerClass ("wasm").
func (o ProcessOptions) EffectiveWorkerClass() string {
	if o.WorkerClass != "" {
		return o.WorkerClass
	}
	return DefaultProcessWorkerClass
}

// EffectiveTransport returns the configured transport or defaults to TransportTypePayload.
func (c *ProcessConfig) EffectiveTransport() string {
	if c.Transport == "" {
		return TransportTypePayload
	}
	return c.Transport
}

// Options returns the resolved execution controls from meta.options with defaults applied.
func (c *ProcessConfig) Options() ProcessOptions {
	if c.options != nil {
		return *c.options
	}
	opts, err := parseAndValidateOptions(c.Meta)
	if err != nil {
		return defaultProcessOptions()
	}
	return opts
}

// Limits returns the resolved actor limits controls.
func (c *ProcessConfig) Limits() ProcessLimitsConfig {
	return c.Options().Limits
}

// Mailbox returns the resolved actor mailbox controls.
func (c *ProcessConfig) Mailbox() ProcessMailboxConfig {
	return c.Options().Mailbox
}

// WorkerClass returns the resolved scheduler worker class (defaults to "wasm").
func (c *ProcessConfig) WorkerClass() string {
	return c.Options().EffectiveWorkerClass()
}

// EffectiveLimitsConfig maps actor limits into a runtime LimitsConfig for use by the WASM engine factory.
func (c *ProcessConfig) EffectiveLimitsConfig() LimitsConfig {
	lim := c.Limits()
	return LimitsConfig{
		MaxExecutionMS:  lim.EffectiveMaxExecutionMS(),
		MaxOpenSockets:  lim.EffectiveMaxOpenSockets(),
		SocketTimeoutMS: lim.EffectiveSocketTimeoutMS(),
	}
}

// SetOptions programmatically sets execution options on the configuration and updates meta.options.
func (c *ProcessConfig) SetOptions(opts ProcessOptions) {
	c.options = &opts
	if c.Meta == nil {
		c.Meta = attrs.NewBag()
	}
	optMap := map[string]any{
		"limits": map[string]any{
			"memory_bytes":      opts.Limits.EffectiveMemoryBytes(),
			"max_execution_ms":  opts.Limits.EffectiveMaxExecutionMS(),
			"max_open_sockets":  opts.Limits.EffectiveMaxOpenSockets(),
			"socket_timeout_ms": opts.Limits.EffectiveSocketTimeoutMS(),
		},
		"mailbox": map[string]any{
			"capacity":      opts.Mailbox.EffectiveCapacity(),
			"bytes":         opts.Mailbox.EffectiveBytes(),
			"message_bytes": opts.Mailbox.EffectiveMessageBytes(),
		},
		"worker_class": opts.EffectiveWorkerClass(),
	}
	c.Meta.Set("options", optMap)
}

// Validate verifies the static code configuration and meta.options execution controls.
// It enforces:
// - Rejection of root-level limits or pool
// - Required static fields (fs, path, hash, method) and valid wasi/imports/transport
// - Strict types and bounded values in meta.options
// - Rejection of unknown fields inside controls
// - Mailbox budget consistency (message_bytes <= bytes)
func (c *ProcessConfig) Validate() error {
	if c.RootLimits != nil || c.hasRootLimits {
		return ErrProcessRootLimitsForbidden
	}
	if c.RootPool != nil || c.hasRootPool {
		return ErrProcessRootPoolForbidden
	}
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
	if err := validateWASI(c.WASI); err != nil {
		return err
	}

	opts, err := parseAndValidateOptions(c.Meta)
	if err != nil {
		return err
	}
	c.options = &opts
	return nil
}

func defaultProcessOptions() ProcessOptions {
	return ProcessOptions{
		Limits: ProcessLimitsConfig{
			MemoryBytes:     DefaultProcessMemoryBytes,
			MaxExecutionMS:  0,
			MaxOpenSockets:  DefaultMaxOpenSockets,
			SocketTimeoutMS: DefaultSocketTimeoutMS,
		},
		Mailbox: ProcessMailboxConfig{
			Capacity:     DefaultProcessMailboxCapacity,
			Bytes:        DefaultProcessMailboxBytes,
			MessageBytes: DefaultProcessMailboxMsgBytes,
		},
		WorkerClass: DefaultProcessWorkerClass,
	}
}

func parseAndValidateOptions(meta attrs.Bag) (ProcessOptions, error) {
	opts := defaultProcessOptions()
	if meta == nil {
		return opts, nil
	}

	optVal, exists := meta.Get("options")
	if !exists || optVal == nil {
		return opts, nil
	}

	// Handle pre-parsed ProcessOptions or *ProcessOptions struct
	switch v := optVal.(type) {
	case ProcessOptions:
		return validateOptionsStruct(v)
	case *ProcessOptions:
		if v == nil {
			return opts, nil
		}
		return validateOptionsStruct(*v)
	}

	// Otherwise must be map-like (map[string]any or attrs.Bag)
	var optMap map[string]any
	switch v := optVal.(type) {
	case attrs.Bag:
		optMap = map[string]any(v)
	case map[string]any:
		optMap = v
	default:
		return opts, ErrProcessOptionsInvalidType
	}

	for k := range optMap {
		switch k {
		case "limits", "mailbox", "worker_class":
		default:
			return opts, newUnknownFieldError("meta.options", k)
		}
	}

	// Worker class
	if wcVal, ok := optMap["worker_class"]; ok && wcVal != nil {
		wcStr, ok := wcVal.(string)
		if !ok {
			return opts, ErrProcessWorkerClassInvalidType
		}
		if wcStr != "" {
			opts.WorkerClass = wcStr
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
			return opts, ErrProcessLimitsInvalidType
		}

		for k := range limMap {
			switch k {
			case "memory_bytes", "max_execution_ms", "max_open_sockets", "socket_timeout_ms":
			default:
				return opts, newUnknownFieldError("meta.options.limits", k)
			}
		}

		if v, ok := limMap["memory_bytes"]; ok && v != nil {
			mb, err := asExactInt64(v, "meta.options.limits.memory_bytes")
			if err != nil {
				return opts, err
			}
			if mb <= 0 {
				return opts, ErrProcessMemoryBytesInvalid
			}
			if mb > MaxProcessMemoryBytes {
				return opts, ErrProcessMemoryBytesExceeded
			}
			if mb%MinProcessMemoryBytesMultiple != 0 {
				return opts, ErrProcessMemoryBytesAlignment
			}
			opts.Limits.MemoryBytes = mb
		}

		if v, ok := limMap["max_execution_ms"]; ok && v != nil {
			ms, err := asExactInt(v, "meta.options.limits.max_execution_ms")
			if err != nil {
				return opts, err
			}
			if ms < 0 || int64(ms) > math.MaxInt64/int64(time.Millisecond) {
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
			if t < 0 || int64(t) > math.MaxInt64/int64(time.Millisecond) {
				return opts, ErrInvalidSocketTimeout
			}
			opts.Limits.SocketTimeoutMS = t
		}
	}

	// Mailbox
	if mbVal, ok := optMap["mailbox"]; ok && mbVal != nil {
		var mbMap map[string]any
		switch v := mbVal.(type) {
		case attrs.Bag:
			mbMap = map[string]any(v)
		case map[string]any:
			mbMap = v
		default:
			return opts, ErrProcessMailboxInvalidType
		}

		for k := range mbMap {
			switch k {
			case "capacity", "bytes", "message_bytes":
			default:
				return opts, newUnknownFieldError("meta.options.mailbox", k)
			}
		}

		if v, ok := mbMap["capacity"]; ok && v != nil {
			capVal, err := asExactInt(v, "meta.options.mailbox.capacity")
			if err != nil {
				return opts, err
			}
			if capVal <= 0 {
				return opts, ErrProcessMailboxCapacityInvalid
			}
			opts.Mailbox.Capacity = capVal
		}

		if v, ok := mbMap["bytes"]; ok && v != nil {
			bVal, err := asExactInt64(v, "meta.options.mailbox.bytes")
			if err != nil {
				return opts, err
			}
			if bVal <= 0 {
				return opts, ErrProcessMailboxBytesInvalid
			}
			opts.Mailbox.Bytes = bVal
		}

		if v, ok := mbMap["message_bytes"]; ok && v != nil {
			msgVal, err := asExactInt64(v, "meta.options.mailbox.message_bytes")
			if err != nil {
				return opts, err
			}
			if msgVal <= 0 {
				return opts, ErrProcessMailboxMessageBytesInvalid
			}
			opts.Mailbox.MessageBytes = msgVal
		}
	}

	if opts.Mailbox.MessageBytes > opts.Mailbox.Bytes || int64(opts.Mailbox.Capacity) > opts.Mailbox.Bytes/256 {
		return opts, ErrProcessMailboxBudgetInconsistent
	}

	return opts, nil
}

func validateOptionsStruct(opts ProcessOptions) (ProcessOptions, error) {
	resolved := defaultProcessOptions()

	if opts.WorkerClass != "" {
		resolved.WorkerClass = opts.WorkerClass
	}

	if opts.Limits.MemoryBytes != 0 {
		mb := opts.Limits.MemoryBytes
		if mb <= 0 {
			return resolved, ErrProcessMemoryBytesInvalid
		}
		if mb > MaxProcessMemoryBytes {
			return resolved, ErrProcessMemoryBytesExceeded
		}
		if mb%MinProcessMemoryBytesMultiple != 0 {
			return resolved, ErrProcessMemoryBytesAlignment
		}
		resolved.Limits.MemoryBytes = mb
	}

	if opts.Limits.MaxExecutionMS < 0 || int64(opts.Limits.MaxExecutionMS) > math.MaxInt64/int64(time.Millisecond) {
		return resolved, ErrInvalidExecutionLimit
	}
	resolved.Limits.MaxExecutionMS = opts.Limits.MaxExecutionMS

	if opts.Limits.MaxOpenSockets < 0 {
		return resolved, ErrInvalidMaxOpenSockets
	}
	if opts.Limits.MaxOpenSockets > 0 {
		resolved.Limits.MaxOpenSockets = opts.Limits.MaxOpenSockets
	}

	if opts.Limits.SocketTimeoutMS < 0 || int64(opts.Limits.SocketTimeoutMS) > math.MaxInt64/int64(time.Millisecond) {
		return resolved, ErrInvalidSocketTimeout
	}
	if opts.Limits.SocketTimeoutMS > 0 {
		resolved.Limits.SocketTimeoutMS = opts.Limits.SocketTimeoutMS
	}

	if opts.Mailbox.Capacity != 0 {
		if opts.Mailbox.Capacity <= 0 {
			return resolved, ErrProcessMailboxCapacityInvalid
		}
		resolved.Mailbox.Capacity = opts.Mailbox.Capacity
	}

	if opts.Mailbox.Bytes != 0 {
		if opts.Mailbox.Bytes <= 0 {
			return resolved, ErrProcessMailboxBytesInvalid
		}
		resolved.Mailbox.Bytes = opts.Mailbox.Bytes
	}

	if opts.Mailbox.MessageBytes != 0 {
		if opts.Mailbox.MessageBytes <= 0 {
			return resolved, ErrProcessMailboxMessageBytesInvalid
		}
		resolved.Mailbox.MessageBytes = opts.Mailbox.MessageBytes
	}

	if resolved.Mailbox.MessageBytes > resolved.Mailbox.Bytes || int64(resolved.Mailbox.Capacity) > resolved.Mailbox.Bytes/256 {
		return resolved, ErrProcessMailboxBudgetInconsistent
	}

	return resolved, nil
}

const maxSafeFloatInt = float64(1<<53 - 1)

func asExactInt64(val any, path string) (int64, error) {
	switch v := val.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case int32:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case uint:
		return int64(v), nil
	case uint64:
		if v > math.MaxInt64 {
			return 0, apierror.New(apierror.Invalid, fmt.Sprintf("%s value overflows int64", path)).WithRetryable(apierror.False)
		}
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Trunc(v) != v {
			return 0, apierror.New(apierror.Invalid, fmt.Sprintf("%s must be an integer, got %v", path, v)).WithRetryable(apierror.False)
		}
		if v < -maxSafeFloatInt || v > maxSafeFloatInt {
			return 0, apierror.New(apierror.Invalid, fmt.Sprintf("%s out of safe integer range", path)).WithRetryable(apierror.False)
		}
		return int64(v), nil
	case float32:
		vf := float64(v)
		if math.IsNaN(vf) || math.IsInf(vf, 0) || math.Trunc(vf) != vf {
			return 0, apierror.New(apierror.Invalid, fmt.Sprintf("%s must be an integer, got %v", path, v)).WithRetryable(apierror.False)
		}
		return int64(vf), nil
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, apierror.New(apierror.Invalid, fmt.Sprintf("%s must be an integer: %v", path, err)).WithRetryable(apierror.False)
		}
		return i, nil
	default:
		return 0, apierror.New(apierror.Invalid, fmt.Sprintf("%s must be an integer, got %T", path, val)).WithRetryable(apierror.False)
	}
}

func asExactInt(val any, path string) (int, error) {
	i64, err := asExactInt64(val, path)
	if err != nil {
		return 0, err
	}
	if i64 < math.MinInt || i64 > math.MaxInt {
		return 0, apierror.New(apierror.Invalid, fmt.Sprintf("%s value overflows int", path)).WithRetryable(apierror.False)
	}
	return int(i64), nil
}
