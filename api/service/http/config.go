// SPDX-License-Identifier: MPL-2.0

// Package http provides HTTP service configuration.
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Registry kind constants for HTTP service components.
// These identify different types of HTTP-related components in the registry.
const (
	// Server identifies an HTTP server component
	Server registry.Kind = "http.service"
	// Router identifies an HTTP router component
	Router registry.Kind = "http.router"
	// Endpoint identifies an HTTP endpoint component
	Endpoint registry.Kind = "http.endpoint"
	// Static identifies a static file server component
	Static registry.Kind = "http.static"

	// ServerID is the key used to identify the server in configuration metadata
	ServerID string = "server"
	// RouterID is the key used to identify the router in configuration metadata
	RouterID string = "router"
)

// TLSMode selects how TLS is terminated for an HTTP service.
type TLSMode string

const (
	TLSModeOff    TLSMode = ""
	TLSModeAuto   TLSMode = "auto"
	TLSModeManual TLSMode = "manual"
)

// ClientAuthType selects how the server verifies client certificates (mTLS).
// Maps onto crypto/tls ClientAuthType. Empty means "no client certs", the
// non-mTLS default.
type ClientAuthType string

const (
	ClientAuthNone             ClientAuthType = ""
	ClientAuthRequest          ClientAuthType = "request"
	ClientAuthRequireAny       ClientAuthType = "require_any"
	ClientAuthVerifyIfGiven    ClientAuthType = "verify_if_given"
	ClientAuthRequireAndVerify ClientAuthType = "require_and_verify"
)

// ServerConfig represents the initial configuration for the Timeouts service.
type (
	ServerConfig struct {
		Meta      attrs.Bag                  `json:"meta"`
		TLS       ServerTLSConfig            `json:"tls"`
		Network   registry.ID                `json:"network"`
		Addr      string                     `json:"addr"`
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
		Timeouts  TimeoutConfig              `json:"timeouts"`
		Host      HostConfig                 `json:"host"`
	}

	// ServerTLSConfig configures TLS termination for an HTTP service.
	//
	// For TLSModeManual, the cert/key pair is supplied via exactly one of:
	//   - Cert + Key: PEM content. Typically populated by the file://
	//     interpolator (manifest-relative, traversal-safe) at config-decode
	//     time, but inline PEM strings also work.
	//   - CertEnv + KeyEnv: env variable names resolved from the Wippy
	//     env.Registry (secure store) at bind time. The data.tls.*_env
	//     wildcard dep pattern ensures the referenced env entries are
	//     registered before this service boots.
	//
	// Optional mTLS (Manual only) requires ClientAuth plus one of ClientCA
	// or ClientCAEnv (a PEM bundle of trusted client CAs).
	ServerTLSConfig struct {
		Mode        TLSMode        `json:"mode"`
		Cert        string         `json:"cert,omitempty"`
		Key         string         `json:"key,omitempty"`
		CertEnv     string         `json:"cert_env,omitempty"`
		KeyEnv      string         `json:"key_env,omitempty"`
		ClientCA    string         `json:"client_ca,omitempty"`
		ClientCAEnv string         `json:"client_ca_env,omitempty"`
		ClientAuth  ClientAuthType `json:"client_auth,omitempty"`
	}

	HostConfig struct {
		BufferSize  int `json:"buffer_size"`  // Internal job channel buffer size
		WorkerCount int `json:"worker_count"` // Number of concurrent worker goroutines
	}

	// TimeoutConfig represents global Timeouts-level configuration options.
	TimeoutConfig struct {
		ReadTimeout  time.Duration `json:"read,omitzero,format:units"`
		WriteTimeout time.Duration `json:"write,omitzero,format:units"`
		IdleTimeout  time.Duration `json:"idle,omitzero,format:units"`
	}

	// RouterConfig represents the configuration for a group of endpoints (a router).
	RouterConfig struct {
		Meta           attrs.Bag         `json:"meta"`
		Options        map[string]string `json:"options"`
		PostOptions    map[string]string `json:"post_options"`
		Server         registry.ID       `json:"server"`
		Prefix         string            `json:"prefix"`
		Middleware     []string          `json:"middleware"`
		PostMiddleware []string          `json:"post_middleware"`
	}

	// EndpointConfig represents the configuration for a single endpoint.
	EndpointConfig struct {
		Meta   attrs.Bag   `json:"meta"`   // Metadata, todo: migrate to avoid use of this
		Path   string      `json:"path"`   // URL path
		Method string      `json:"method"` // Timeouts method
		Func   registry.ID `json:"func"`   // Func function
	}

	// StaticConfig represents the configuration for a static file server endpoint
	StaticConfig struct {
		Meta          attrs.Bag         `json:"meta"`
		Options       map[string]string `json:"options"`
		FS            registry.ID       `json:"fs"`
		StaticOptions StaticOptions     `json:"static_options"`
		Path          string            `json:"path"`
		Directory     string            `json:"directory"`
		Middleware    []string          `json:"middleware"`
	}

	StaticOptions struct {
		IndexFile    string `json:"index"`
		CacheControl string `json:"cache"`
		SPA          bool   `json:"spa"`
	}
)

// SetMeta sets the metadata for ServerConfig
func (c *ServerConfig) SetMeta(meta attrs.Bag) {
	if c.Meta == nil { // todo: remove later once we migrate away from using meta for config!
		c.Meta = meta
	}
}

// SetMeta sets the metadata for RouterConfig
func (c *RouterConfig) SetMeta(meta attrs.Bag) {
	if c.Meta == nil { // todo: remove later once we migrate away from using meta for config!
		c.Meta = meta
	}
}

// SetMeta sets the metadata for EndpointConfig
func (c *EndpointConfig) SetMeta(meta attrs.Bag) {
	if c.Meta == nil { // todo: remove later once we migrate away from using meta for config!
		c.Meta = meta
	}
}

// SetMeta sets the metadata for StaticConfig
func (c *StaticConfig) SetMeta(meta attrs.Bag) {
	if c.Meta == nil { // todo: remove later once we migrate away from using meta for config!
		c.Meta = meta
	}
}

// Validate checks if the server configuration is valid
func (c *ServerConfig) Validate() error {
	if c.Addr == "" {
		return ErrEmptyAddr
	}

	if err := c.Timeouts.Validate(); err != nil {
		return NewInvalidTimeoutConfigError(err)
	}

	if c.Lifecycle.StartTimeout < 0 {
		return NewInvalidTimeoutError("start timeout")
	}

	if c.Lifecycle.StopTimeout < 0 {
		return NewInvalidTimeoutError("stop timeout")
	}

	if c.Host.BufferSize < 0 {
		return NewNegativeConfigError("buffer size")
	}

	if c.Host.WorkerCount < 0 {
		return NewNegativeConfigError("worker count")
	}

	if err := c.TLS.Validate(); err != nil {
		return err
	}

	return nil
}

// Validate checks if the TLS configuration is internally consistent.
// Network/driver compatibility and env-var resolution are enforced at
// server start.
func (c *ServerTLSConfig) Validate() error {
	switch c.Mode {
	case TLSModeOff:
		if c.hasAnyCertInput() || c.hasAnyMTLSInput() {
			return ErrTLSOffHasInputs
		}
		return nil

	case TLSModeAuto:
		if c.hasAnyCertInput() {
			return ErrTLSAutoHasCertInputs
		}
		if c.hasAnyMTLSInput() {
			return ErrTLSMTLSRequiresManual
		}
		return nil

	case TLSModeManual:
		// Partial pairs take precedence over the generic "missing" message
		// so users get an error pointing at the exact field they forgot.
		if (c.Cert != "") != (c.Key != "") {
			return ErrTLSManualPartialCert
		}
		if (c.CertEnv != "") != (c.KeyEnv != "") {
			return ErrTLSManualPartialCertEnv
		}
		hasInline := c.Cert != "" && c.Key != ""
		hasEnv := c.CertEnv != "" && c.KeyEnv != ""
		if !hasInline && !hasEnv {
			return ErrTLSManualMissingCert
		}
		if hasInline && hasEnv {
			return ErrTLSManualAmbiguousCert
		}
		return c.validateClientAuth()

	default:
		return NewInvalidTLSModeError(string(c.Mode))
	}
}

func (c *ServerTLSConfig) hasAnyCertInput() bool {
	return c.Cert != "" || c.Key != "" || c.CertEnv != "" || c.KeyEnv != ""
}

func (c *ServerTLSConfig) hasAnyMTLSInput() bool {
	return c.ClientCA != "" || c.ClientCAEnv != "" || c.ClientAuth != ClientAuthNone
}

// validateClientAuth enforces mTLS invariants under TLSModeManual:
//   - ClientAuth, when set, must be a known value
//   - Verifying modes (verify_if_given, require_and_verify) require a CA
//   - ClientCA and ClientCAEnv are mutually exclusive
func (c *ServerTLSConfig) validateClientAuth() error {
	if c.ClientCA != "" && c.ClientCAEnv != "" {
		return ErrTLSMTLSAmbiguousCA
	}
	switch c.ClientAuth {
	case ClientAuthNone:
		if c.ClientCA != "" || c.ClientCAEnv != "" {
			return ErrTLSMTLSCAWithoutAuth
		}
		return nil
	case ClientAuthRequest, ClientAuthRequireAny:
		// These modes do not verify against a CA pool; a pool is optional.
		return nil
	case ClientAuthVerifyIfGiven, ClientAuthRequireAndVerify:
		if c.ClientCA == "" && c.ClientCAEnv == "" {
			return ErrTLSMTLSMissingCA
		}
		return nil
	default:
		return NewInvalidClientAuthError(string(c.ClientAuth))
	}
}

// Validate checks if the timeout configuration is valid
func (c *TimeoutConfig) Validate() error {
	if c.ReadTimeout < 0 {
		return NewInvalidTimeoutError("read timeout")
	}
	if c.WriteTimeout < 0 {
		return NewInvalidTimeoutError("write timeout")
	}
	if c.IdleTimeout < 0 {
		return NewInvalidTimeoutError("idle timeout")
	}
	return nil
}

// Validate checks if the router configuration is valid
func (c *RouterConfig) Validate() error {
	if c.Meta == nil {
		return ErrNilMetadata
	}

	serverID := c.Meta.GetString(ServerID, "")
	if serverID == "" {
		return NewMissingMetadataError("server")
	}

	return nil
}

// Validate checks if the endpoint configuration is valid
func (c *EndpointConfig) Validate() error {
	if c.Func.Name == "" {
		return ErrEmptyFuncName
	}

	if c.Path == "" {
		return ErrEmptyPath
	}

	if !strings.HasPrefix(c.Path, "/") {
		return NewPathMustStartWithSlashError()
	}

	if c.Method == "" {
		return ErrEmptyMethod
	}

	switch c.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodHead, http.MethodOptions, http.MethodTrace:
	default:
		return NewInvalidHTTPMethodError(c.Method)
	}

	if c.Meta == nil {
		return ErrNilMetadata
	}

	routerID := c.Meta.GetString(RouterID, "")
	if routerID == "" {
		return NewMissingMetadataError("router")
	}

	return nil
}

// Validate checks if the endpoint configuration is valid
func (c *StaticConfig) Validate() error {
	if c.Path == "" {
		return ErrEmptyPath
	}

	if !strings.HasPrefix(c.Path, "/") {
		return NewPathMustStartWithSlashError()
	}

	if c.Meta == nil {
		return ErrNilMetadata
	}

	serverID := c.Meta.GetString(ServerID, "")
	if serverID == "" {
		return NewMissingMetadataError("server")
	}

	return nil
}

// UnmarshalJSON implements custom unmarshaling for StaticConfig to handle backward compatibility.
// Migrates legacy options like "spa" from options map to static_options struct.
func (c *StaticConfig) UnmarshalJSON(data []byte) error {
	type Alias StaticConfig
	aux := &struct {
		Options map[string]any `json:"options"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Migrate legacy options from map to StaticOptions struct
	if aux.Options != nil {
		if c.Options == nil {
			c.Options = make(map[string]string)
		}

		for key, val := range aux.Options {
			switch key {
			case "spa":
				// Migrate spa from options map to static_options.spa
				if boolVal, ok := val.(bool); ok {
					c.StaticOptions.SPA = boolVal
				} else if strVal, ok := val.(string); ok {
					// Support string "true"/"false" for backward compatibility
					c.StaticOptions.SPA = strVal == "true"
				}
			case "index":
				// Migrate index from options map to static_options.index
				if strVal, ok := val.(string); ok {
					c.StaticOptions.IndexFile = strVal
				}
			case "cache":
				// Migrate cache from options map to static_options.cache
				if strVal, ok := val.(string); ok {
					c.StaticOptions.CacheControl = strVal
				}
			default:
				// Keep other options in the map as middleware options
				if strVal, ok := val.(string); ok {
					c.Options[key] = strVal
				} else {
					c.Options[key] = anyToString(val)
				}
			}
		}
	}

	return nil
}

func anyToString(val any) string {
	return fmt.Sprintf("%v", val)
}

// MarshalJSON implements custom marshaling for TimeoutConfig to output durations as strings.
func (c *TimeoutConfig) MarshalJSON() ([]byte, error) {
	m := make(map[string]string)
	if c.ReadTimeout != 0 {
		m["read"] = c.ReadTimeout.String()
	}
	if c.WriteTimeout != 0 {
		m["write"] = c.WriteTimeout.String()
	}
	if c.IdleTimeout != 0 {
		m["idle"] = c.IdleTimeout.String()
	}
	return json.Marshal(m)
}

// UnmarshalJSON implements custom unmarshaling for TimeoutConfig to parse duration strings.
func (c *TimeoutConfig) UnmarshalJSON(data []byte) error {
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	if v, ok := m["read"]; ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid read timeout: %w", err)
		}
		c.ReadTimeout = d
	}
	if v, ok := m["write"]; ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid write timeout: %w", err)
		}
		c.WriteTimeout = d
	}
	if v, ok := m["idle"]; ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid idle timeout: %w", err)
		}
		c.IdleTimeout = d
	}
	return nil
}
