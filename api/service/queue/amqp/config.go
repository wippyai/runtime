// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"time"

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
	// Lifecycle configures the supervisor lifecycle for this driver.
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`

	// TLS configures TLS/SSL connection settings.
	// When TLS.Enabled is true, connections will use TLS (amqps://).
	TLS *TLSConfig `json:"tls,omitempty"`

	// URL is the AMQP connection URL.
	// Format: amqp://user:pass@host:port/vhost
	URL string `json:"url"`

	// Vhost overrides the virtual host from the URL.
	// If empty, the vhost from the URL is used.
	Vhost string `json:"vhost,omitempty"`

	// ConnectionName identifies this connection in the RabbitMQ management UI.
	// Sets the "connection_name" property on the AMQP connection.
	ConnectionName string `json:"connection_name,omitempty"`

	// AuthMechanism selects the SASL authentication mechanism.
	// Supported values: "PLAIN" (default), "EXTERNAL" (for mTLS), "AMQPLAIN".
	// When empty, defaults to PLAIN (credentials from URL).
	AuthMechanism string `json:"auth_mechanism,omitempty"`

	// Heartbeat is the interval for AMQP heartbeat frames.
	// Heartbeats detect dead TCP connections. Default: server negotiated (~60s).
	Heartbeat time.Duration `json:"heartbeat,omitzero,format:units"`

	// ConnectionTimeout is the timeout for establishing the TCP connection.
	// Default: 30s (amqp091 library default).
	ConnectionTimeout time.Duration `json:"connection_timeout,omitzero,format:units"`

	// DefaultMessageTTL is the default per-message TTL applied on publish.
	// Sets the Expiration field on amqp091.Publishing.
	// Only applied when the message does not have a HeaderTTL header.
	// 0 means no default message TTL.
	DefaultMessageTTL time.Duration `json:"default_message_ttl,omitzero,format:units"`

	// DefaultQueueTTL is the default queue-level message TTL argument (x-message-ttl).
	// All messages delivered to the queue expire after this duration.
	// Passed as queue argument on QueueDeclare. 0 means no queue message TTL.
	DefaultQueueTTL time.Duration `json:"default_queue_ttl,omitzero,format:units"`

	// DefaultQueueExpiry is the unused queue expiry (x-expires).
	// A queue is automatically deleted after being unused for this duration.
	// Passed as queue argument on QueueDeclare. 0 means the queue never expires.
	DefaultQueueExpiry time.Duration `json:"default_queue_expiry,omitzero,format:units"`

	// FrameSize is the maximum frame size in bytes negotiated with the server.
	// 0 uses the library default (131072 bytes).
	FrameSize int `json:"frame_size,omitempty"`

	// PrefetchCount is the number of unacknowledged messages the server will
	// deliver per channel before requiring acknowledgments (QoS).
	// Maps to Channel.Qos(prefetchCount, 0, false) in AMQP.
	// 0 means no limit (server default — unlimited prefetch).
	// Typical production value: 10–50.
	PrefetchCount int `json:"prefetch_count,omitempty"`

	// ChannelMax is the maximum number of channels per connection.
	// 0 means no application-side limit (server may impose its own).
	ChannelMax uint16 `json:"channel_max,omitempty"`
}

// TLSConfig defines TLS connection settings for AMQP.
type TLSConfig struct {
	// ServerName overrides the server name used for TLS certificate verification.
	ServerName string `json:"server_name,omitempty"`

	// CertFile is the path to the client certificate file (PEM format) for mTLS.
	CertFile string `json:"cert_file,omitempty"`

	// KeyFile is the path to the client private key file (PEM format) for mTLS.
	KeyFile string `json:"key_file,omitempty"`

	// CAFile is the path to the CA certificate file (PEM format)
	// for verifying the server's certificate.
	CAFile string `json:"ca_file,omitempty"`

	// Enabled activates TLS for AMQP connections.
	Enabled bool `json:"enabled"`

	// InsecureSkipVerify skips TLS certificate verification.
	// WARNING: This makes the connection susceptible to man-in-the-middle attacks.
	InsecureSkipVerify bool `json:"insecure_skip_verify,omitempty"`
}

// BuildTLSConfig converts the TLSConfig into a Go *tls.Config.
func (t *TLSConfig) BuildTLSConfig() (*tls.Config, error) {
	if t == nil || !t.Enabled {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: t.InsecureSkipVerify, //nolint:gosec // G402: user-configurable for dev/testing
		ServerName:         t.ServerName,
	}

	// Load client certificate for mTLS
	if t.CertFile != "" && t.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("amqp tls: load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate
	if t.CAFile != "" {
		caCert, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("amqp tls: read ca file: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("amqp tls: failed to parse ca certificate")
		}
		tlsCfg.RootCAs = caCertPool
	}

	return tlsCfg, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.URL == "" {
		return fmt.Errorf("amqp: url is required")
	}
	if c.TLS != nil && c.TLS.CertFile != "" && c.TLS.KeyFile == "" {
		return fmt.Errorf("amqp: tls cert_file requires key_file")
	}
	if c.TLS != nil && c.TLS.KeyFile != "" && c.TLS.CertFile == "" {
		return fmt.Errorf("amqp: tls key_file requires cert_file")
	}
	switch c.AuthMechanism {
	case "", "PLAIN", "EXTERNAL", "AMQPLAIN":
		// valid
	default:
		return fmt.Errorf("amqp: unsupported auth_mechanism %q (supported: PLAIN, EXTERNAL, AMQPLAIN)", c.AuthMechanism)
	}
	return nil
}

// InitDefaults initializes default values.
func (c *Config) InitDefaults() {
	if c.URL == "" {
		c.URL = "amqp://guest:guest@localhost:5672/"
	}
}

// configJSON is a shadow struct for JSON marshaling/unmarshaling
// of duration fields.
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

	// Parse duration fields
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

	return json.Marshal(raw)
}
