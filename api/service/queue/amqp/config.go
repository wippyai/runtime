// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"time"

	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Kind identifies the AMQP (RabbitMQ) queue driver.
const Kind registry.Kind = "queue.driver.amqp"

// Config defines the AMQP queue driver configuration.
//
// Connection options map to amqp091.Config fields used with DialConfig.
// TTL fields configure default message/queue expiration behavior.
type Config struct { //nolint:govet // fieldalignment: limited by LifecycleConfig embedded struct layout
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`

	TLS *TLSConfig `json:"tls,omitempty"`

	URL string `json:"url"`

	Vhost string `json:"vhost,omitempty"`

	ConnectionName string `json:"connection_name,omitempty"`

	// AuthMechanism selects the SASL authentication mechanism.
	// Supported: "PLAIN" (default), "EXTERNAL" (for mTLS), "AMQPLAIN".
	AuthMechanism string `json:"auth_mechanism,omitempty"`

	Heartbeat time.Duration `json:"heartbeat,omitzero,format:units"`

	ConnectionTimeout time.Duration `json:"connection_timeout,omitzero,format:units"`

	DefaultMessageTTL time.Duration `json:"default_message_ttl,omitzero,format:units"`

	DefaultQueueTTL time.Duration `json:"default_queue_ttl,omitzero,format:units"`

	DefaultQueueExpiry time.Duration `json:"default_queue_expiry,omitzero,format:units"`

	ReconnectDelay time.Duration `json:"reconnect_delay,omitzero,format:units"`

	ReconnectMaxDelay time.Duration `json:"reconnect_max_delay,omitzero,format:units"`

	FrameSize int `json:"frame_size,omitempty"`

	PrefetchCount int `json:"prefetch_count,omitempty"`

	ChannelMax uint16 `json:"channel_max,omitempty"`
}

// TLSConfig defines TLS connection settings for AMQP.
//
// Cert/Key/CA carry inline PEM content (the config-decode file:// interpolator
// can populate them from disk at decode time). CertEnv/KeyEnv/CAEnv name env
// variables resolved from the Wippy env.Registry at driver start.
// Inline and env sources are mutually exclusive per field.
type TLSConfig struct {
	ServerName string `json:"server_name,omitempty"`

	Cert    string `json:"cert,omitempty"`
	CertEnv string `json:"cert_env,omitempty"`

	Key    string `json:"key,omitempty"`
	KeyEnv string `json:"key_env,omitempty"`

	CA    string `json:"ca,omitempty"`
	CAEnv string `json:"ca_env,omitempty"`

	Enabled bool `json:"enabled"`

	InsecureSkipVerify bool `json:"insecure_skip_verify,omitempty"`
}

// BuildTLSConfig converts the TLSConfig into a Go *tls.Config. CertEnv/KeyEnv/
// CAEnv are resolved via envapi.GetRegistry(ctx); inline Cert/Key/CA are used
// directly. Returns (nil, nil) when TLS is not enabled.
func (t *TLSConfig) BuildTLSConfig(ctx context.Context) (*tls.Config, error) {
	if t == nil || !t.Enabled {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: t.InsecureSkipVerify, //nolint:gosec // G402: user-configurable for dev/testing
		ServerName:         t.ServerName,
	}

	certPEM, err := resolveEnvOrInline(ctx, "cert", t.Cert, t.CertEnv)
	if err != nil {
		return nil, err
	}
	keyPEM, err := resolveEnvOrInline(ctx, "key", t.Key, t.KeyEnv)
	if err != nil {
		return nil, err
	}
	if len(certPEM) > 0 && len(keyPEM) > 0 {
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("amqp tls: load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	caPEM, err := resolveEnvOrInline(ctx, "ca", t.CA, t.CAEnv)
	if err != nil {
		return nil, err
	}
	if len(caPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("amqp tls: failed to parse ca certificate")
		}
		tlsCfg.RootCAs = pool
	}

	return tlsCfg, nil
}

// resolveEnvOrInline returns the PEM bytes for one TLS material (cert, key, ca).
// Inline value wins if present; otherwise an env-var name is resolved via
// envapi.Registry. An empty result is legal — the caller decides what is
// required.
func resolveEnvOrInline(ctx context.Context, field, inline, envName string) ([]byte, error) {
	if inline != "" {
		return []byte(inline), nil
	}
	if envName == "" {
		return nil, nil
	}
	reg := envapi.GetRegistry(ctx)
	if reg == nil {
		return nil, fmt.Errorf("amqp tls: %s_env %q requested but env registry is unavailable", field, envName)
	}
	val, err := reg.Get(ctx, envName)
	if err != nil {
		return nil, fmt.Errorf("amqp tls: resolve %s_env %q: %w", field, envName, err)
	}
	return []byte(val), nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.URL == "" {
		return fmt.Errorf("amqp: url is required")
	}
	if err := c.TLS.Validate(); err != nil {
		return err
	}
	switch c.AuthMechanism {
	case "", "PLAIN", "EXTERNAL", "AMQPLAIN":
	default:
		return fmt.Errorf("amqp: unsupported auth_mechanism %q (supported: PLAIN, EXTERNAL, AMQPLAIN)", c.AuthMechanism)
	}
	return nil
}

// Validate enforces internal consistency of the TLS block:
// inline/env sources are mutually exclusive per field, and cert+key form a pair.
func (t *TLSConfig) Validate() error {
	if t == nil || !t.Enabled {
		return nil
	}
	if t.Cert != "" && t.CertEnv != "" {
		return fmt.Errorf("amqp tls: cert and cert_env are mutually exclusive")
	}
	if t.Key != "" && t.KeyEnv != "" {
		return fmt.Errorf("amqp tls: key and key_env are mutually exclusive")
	}
	if t.CA != "" && t.CAEnv != "" {
		return fmt.Errorf("amqp tls: ca and ca_env are mutually exclusive")
	}
	hasCert := t.Cert != "" || t.CertEnv != ""
	hasKey := t.Key != "" || t.KeyEnv != ""
	if hasCert != hasKey {
		return fmt.Errorf("amqp tls: cert and key must be provided together")
	}
	return nil
}

// InitDefaults initializes default values.
func (c *Config) InitDefaults() {
	if c.URL == "" {
		c.URL = "amqp://guest:guest@localhost:5672/"
	}
	if c.ReconnectDelay == 0 {
		c.ReconnectDelay = time.Second
	}
	if c.ReconnectMaxDelay == 0 {
		c.ReconnectMaxDelay = 30 * time.Second
	}
}

// configJSON is a shadow struct for JSON marshaling/unmarshaling of duration fields.
type configJSON struct { //nolint:govet // fieldalignment: limited by LifecycleConfig embedded struct layout
	Lifecycle          supervisor.LifecycleConfig `json:"lifecycle"`
	TLS                *TLSConfig                 `json:"tls,omitempty"`
	URL                string                     `json:"url"`
	Vhost              string                     `json:"vhost,omitempty"`
	ConnectionName     string                     `json:"connection_name,omitempty"`
	AuthMechanism      string                     `json:"auth_mechanism,omitempty"`
	Heartbeat          string                     `json:"heartbeat,omitempty"`
	ConnectionTimeout  string                     `json:"connection_timeout,omitempty"`
	DefaultMessageTTL  string                     `json:"default_message_ttl,omitempty"`
	DefaultQueueTTL    string                     `json:"default_queue_ttl,omitempty"`
	DefaultQueueExpiry string                     `json:"default_queue_expiry,omitempty"`
	ReconnectDelay     string                     `json:"reconnect_delay,omitempty"`
	ReconnectMaxDelay  string                     `json:"reconnect_max_delay,omitempty"`
	FrameSize          int                        `json:"frame_size,omitempty"`
	PrefetchCount      int                        `json:"prefetch_count,omitempty"`
	ChannelMax         uint16                     `json:"channel_max,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling for duration fields.
func (c *Config) UnmarshalJSON(data []byte) error {
	var raw configJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.URL = raw.URL
	c.Vhost = raw.Vhost
	c.ConnectionName = raw.ConnectionName
	c.AuthMechanism = raw.AuthMechanism
	c.FrameSize = raw.FrameSize
	c.PrefetchCount = raw.PrefetchCount
	c.ChannelMax = raw.ChannelMax
	c.TLS = raw.TLS
	c.Lifecycle = raw.Lifecycle

	var err error
	if raw.Heartbeat != "" {
		if c.Heartbeat, err = time.ParseDuration(raw.Heartbeat); err != nil {
			return fmt.Errorf("invalid heartbeat: %w", err)
		}
	}
	if raw.ConnectionTimeout != "" {
		if c.ConnectionTimeout, err = time.ParseDuration(raw.ConnectionTimeout); err != nil {
			return fmt.Errorf("invalid connection_timeout: %w", err)
		}
	}
	if raw.DefaultMessageTTL != "" {
		if c.DefaultMessageTTL, err = time.ParseDuration(raw.DefaultMessageTTL); err != nil {
			return fmt.Errorf("invalid default_message_ttl: %w", err)
		}
	}
	if raw.DefaultQueueTTL != "" {
		if c.DefaultQueueTTL, err = time.ParseDuration(raw.DefaultQueueTTL); err != nil {
			return fmt.Errorf("invalid default_queue_ttl: %w", err)
		}
	}
	if raw.DefaultQueueExpiry != "" {
		if c.DefaultQueueExpiry, err = time.ParseDuration(raw.DefaultQueueExpiry); err != nil {
			return fmt.Errorf("invalid default_queue_expiry: %w", err)
		}
	}
	if raw.ReconnectDelay != "" {
		if c.ReconnectDelay, err = time.ParseDuration(raw.ReconnectDelay); err != nil {
			return fmt.Errorf("invalid reconnect_delay: %w", err)
		}
	}
	if raw.ReconnectMaxDelay != "" {
		if c.ReconnectMaxDelay, err = time.ParseDuration(raw.ReconnectMaxDelay); err != nil {
			return fmt.Errorf("invalid reconnect_max_delay: %w", err)
		}
	}

	return nil
}

// MarshalJSON implements custom JSON marshaling for duration fields.
func (c Config) MarshalJSON() ([]byte, error) {
	raw := configJSON{
		URL:            c.URL,
		Vhost:          c.Vhost,
		ConnectionName: c.ConnectionName,
		AuthMechanism:  c.AuthMechanism,
		FrameSize:      c.FrameSize,
		PrefetchCount:  c.PrefetchCount,
		ChannelMax:     c.ChannelMax,
		TLS:            c.TLS,
		Lifecycle:      c.Lifecycle,
	}

	if c.Heartbeat != 0 {
		raw.Heartbeat = c.Heartbeat.String()
	}
	if c.ConnectionTimeout != 0 {
		raw.ConnectionTimeout = c.ConnectionTimeout.String()
	}
	if c.DefaultMessageTTL != 0 {
		raw.DefaultMessageTTL = c.DefaultMessageTTL.String()
	}
	if c.DefaultQueueTTL != 0 {
		raw.DefaultQueueTTL = c.DefaultQueueTTL.String()
	}
	if c.DefaultQueueExpiry != 0 {
		raw.DefaultQueueExpiry = c.DefaultQueueExpiry.String()
	}
	if c.ReconnectDelay != 0 {
		raw.ReconnectDelay = c.ReconnectDelay.String()
	}
	if c.ReconnectMaxDelay != 0 {
		raw.ReconnectMaxDelay = c.ReconnectMaxDelay.String()
	}

	return json.Marshal(raw)
}
