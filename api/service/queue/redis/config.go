// SPDX-License-Identifier: MPL-2.0

package redis

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

// Kind identifies the Redis Streams queue driver.
const Kind registry.Kind = "queue.driver.redis"

// Config defines the Redis Streams queue driver configuration.
//
// The connection mode is determined automatically by the go-redis UniversalClient:
//   - Single address, no MasterName → standalone client
//   - MasterName set → sentinel/failover client
//   - Multiple addresses, no MasterName → cluster client
//   - IsClusterMode set with single address → cluster client (e.g. ElastiCache config endpoint)
type Config struct { //nolint:govet // fieldalignment: limited by LifecycleConfig embedded struct layout
	// Lifecycle configures the supervisor lifecycle for this driver.
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`

	// Addrs is a list of host:port addresses.
	// For standalone: a single address (e.g. ["localhost:6379"]).
	// For cluster: seed addresses of cluster nodes.
	// For sentinel: addresses of sentinel nodes.
	Addrs []string `json:"addrs,omitempty"`

	// TLS configures TLS/SSL connection settings.
	// When TLS.Enabled is true, connections will use TLS.
	TLS *TLSConfig `json:"tls,omitempty"`

	// MasterName is the sentinel master name.
	// When set, the client operates in sentinel/failover mode.
	MasterName string `json:"master_name,omitempty"`

	// ClientName will execute the CLIENT SETNAME command for each connection.
	ClientName string `json:"client_name,omitempty"`

	// Username for ACL-based authentication (Redis 6.0+).
	Username string `json:"username,omitempty"`

	// Password for authentication (requirepass or ACL).
	Password string `json:"password,omitempty"`

	// SentinelUsername for ACL-based authentication with sentinel nodes.
	SentinelUsername string `json:"sentinel_username,omitempty"`

	// SentinelPassword for authentication with sentinel nodes.
	SentinelPassword string `json:"sentinel_password,omitempty"`

	// MinRetryBackoff is the minimum backoff between retries.
	// -1 disables backoff. Default: 8ms.
	MinRetryBackoff time.Duration `json:"min_retry_backoff,omitzero,format:units"`

	// MaxRetryBackoff is the maximum backoff between retries.
	// -1 disables backoff. Default: 512ms.
	MaxRetryBackoff time.Duration `json:"max_retry_backoff,omitzero,format:units"`

	// DialTimeout is the timeout for establishing new connections.
	// Default: 5s.
	DialTimeout time.Duration `json:"dial_timeout,omitzero,format:units"`

	// ReadTimeout is the timeout for socket reads.
	// -1 means no timeout. Default: 3s.
	ReadTimeout time.Duration `json:"read_timeout,omitzero,format:units"`

	// WriteTimeout is the timeout for socket writes.
	// -1 means no timeout. Default: 3s.
	WriteTimeout time.Duration `json:"write_timeout,omitzero,format:units"`

	// PoolTimeout is the amount of time client waits for a free connection.
	// Default: ReadTimeout + 1s.
	PoolTimeout time.Duration `json:"pool_timeout,omitzero,format:units"`

	// ConnMaxIdleTime is the maximum amount of time a connection may be idle.
	// Default: 30m.
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time,omitzero,format:units"`

	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	// 0 means no limit.
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime,omitzero,format:units"`

	// Protocol is the RESP protocol version (2 or 3).
	// Default: 3.
	Protocol int `json:"protocol,omitempty"`

	// DB is the database number to select after connecting.
	// Only applicable to standalone and sentinel modes (not cluster).
	DB int `json:"db,omitempty"`

	// MaxRetries is the maximum number of retries before giving up.
	// -1 disables retries. Default: 3.
	MaxRetries int `json:"max_retries,omitempty"`

	// PoolSize is the maximum number of socket connections.
	// For cluster mode, this applies per cluster node.
	// Default: 10 * runtime.GOMAXPROCS(0).
	PoolSize int `json:"pool_size,omitempty"`

	// MinIdleConns is the minimum number of idle connections.
	MinIdleConns int `json:"min_idle_conns,omitempty"`

	// MaxIdleConns is the maximum number of idle connections.
	MaxIdleConns int `json:"max_idle_conns,omitempty"`

	// MaxActiveConns is the maximum number of connections allocated by the pool.
	// For cluster mode, this applies per cluster node.
	// 0 means no limit.
	MaxActiveConns int `json:"max_active_conns,omitempty"`

	// MaxRedirects is the maximum number of retries on MOVED and ASK redirects.
	// Only applicable to cluster mode. Default: 3.
	MaxRedirects int `json:"max_redirects,omitempty"`

	// ContextTimeoutEnabled controls whether the client respects
	// context timeouts and deadlines.
	ContextTimeoutEnabled bool `json:"context_timeout_enabled,omitempty"`

	// PoolFIFO uses FIFO mode for the connection pool when true.
	// Default: LIFO.
	PoolFIFO bool `json:"pool_fifo,omitempty"`

	// IsClusterMode forces cluster mode even with a single address.
	// Useful for managed Redis services like AWS ElastiCache that
	// expose a single configuration endpoint for cluster mode.
	IsClusterMode bool `json:"is_cluster_mode,omitempty"`
}

// TLSConfig defines TLS connection settings for Redis.
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

	// Enabled activates TLS for Redis connections.
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
			return nil, fmt.Errorf("redis tls: load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate
	if t.CAFile != "" {
		caCert, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("redis tls: read ca file: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("redis tls: failed to parse ca certificate")
		}
		tlsCfg.RootCAs = caCertPool
	}

	return tlsCfg, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if len(c.Addrs) == 0 {
		return fmt.Errorf("redis: at least one address is required")
	}
	if c.TLS != nil && c.TLS.CertFile != "" && c.TLS.KeyFile == "" {
		return fmt.Errorf("redis: tls cert_file requires key_file")
	}
	if c.TLS != nil && c.TLS.KeyFile != "" && c.TLS.CertFile == "" {
		return fmt.Errorf("redis: tls key_file requires cert_file")
	}
	return nil
}

// InitDefaults initializes default values.
func (c *Config) InitDefaults() {
	if len(c.Addrs) == 0 {
		c.Addrs = []string{"localhost:6379"}
	}
}

// configJSON is a shadow struct for JSON marshaling/unmarshaling
// of duration fields and backward compatibility.
type configJSON struct { //nolint:govet // fieldalignment: limited by LifecycleConfig embedded struct layout
	Lifecycle             supervisor.LifecycleConfig `json:"lifecycle"`
	Addrs                 []string                   `json:"addrs,omitempty"`
	TLS                   *TLSConfig                 `json:"tls,omitempty"`
	Addr                  string                     `json:"addr,omitempty"` // backward compat
	MasterName            string                     `json:"master_name,omitempty"`
	ClientName            string                     `json:"client_name,omitempty"`
	Username              string                     `json:"username,omitempty"`
	Password              string                     `json:"password,omitempty"`
	SentinelUsername      string                     `json:"sentinel_username,omitempty"`
	SentinelPassword      string                     `json:"sentinel_password,omitempty"`
	MinRetryBackoff       string                     `json:"min_retry_backoff,omitempty"`
	MaxRetryBackoff       string                     `json:"max_retry_backoff,omitempty"`
	DialTimeout           string                     `json:"dial_timeout,omitempty"`
	ReadTimeout           string                     `json:"read_timeout,omitempty"`
	WriteTimeout          string                     `json:"write_timeout,omitempty"`
	PoolTimeout           string                     `json:"pool_timeout,omitempty"`
	ConnMaxIdleTime       string                     `json:"conn_max_idle_time,omitempty"`
	ConnMaxLifetime       string                     `json:"conn_max_lifetime,omitempty"`
	Protocol              int                        `json:"protocol,omitempty"`
	DB                    int                        `json:"db,omitempty"`
	MaxRetries            int                        `json:"max_retries,omitempty"`
	PoolSize              int                        `json:"pool_size,omitempty"`
	MinIdleConns          int                        `json:"min_idle_conns,omitempty"`
	MaxIdleConns          int                        `json:"max_idle_conns,omitempty"`
	MaxActiveConns        int                        `json:"max_active_conns,omitempty"`
	MaxRedirects          int                        `json:"max_redirects,omitempty"`
	ContextTimeoutEnabled bool                       `json:"context_timeout_enabled,omitempty"`
	PoolFIFO              bool                       `json:"pool_fifo,omitempty"`
	IsClusterMode         bool                       `json:"is_cluster_mode,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling for duration fields
// and backward compatibility with the old "addr" field.
func (c *Config) UnmarshalJSON(data []byte) error {
	var raw configJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Backward compatibility: accept "addr" as alias for "addrs"
	c.Addrs = raw.Addrs
	if len(c.Addrs) == 0 && raw.Addr != "" {
		c.Addrs = []string{raw.Addr}
	}

	c.MasterName = raw.MasterName
	c.ClientName = raw.ClientName
	c.Protocol = raw.Protocol
	c.Username = raw.Username
	c.Password = raw.Password
	c.SentinelUsername = raw.SentinelUsername
	c.SentinelPassword = raw.SentinelPassword
	c.DB = raw.DB
	c.MaxRetries = raw.MaxRetries
	c.ContextTimeoutEnabled = raw.ContextTimeoutEnabled
	c.PoolSize = raw.PoolSize
	c.MinIdleConns = raw.MinIdleConns
	c.MaxIdleConns = raw.MaxIdleConns
	c.MaxActiveConns = raw.MaxActiveConns
	c.PoolFIFO = raw.PoolFIFO
	c.MaxRedirects = raw.MaxRedirects
	c.IsClusterMode = raw.IsClusterMode
	c.TLS = raw.TLS
	c.Lifecycle = raw.Lifecycle

	// Parse duration fields
	var err error
	if raw.MinRetryBackoff != "" {
		if c.MinRetryBackoff, err = time.ParseDuration(raw.MinRetryBackoff); err != nil {
			return fmt.Errorf("invalid min_retry_backoff: %w", err)
		}
	}
	if raw.MaxRetryBackoff != "" {
		if c.MaxRetryBackoff, err = time.ParseDuration(raw.MaxRetryBackoff); err != nil {
			return fmt.Errorf("invalid max_retry_backoff: %w", err)
		}
	}
	if raw.DialTimeout != "" {
		if c.DialTimeout, err = time.ParseDuration(raw.DialTimeout); err != nil {
			return fmt.Errorf("invalid dial_timeout: %w", err)
		}
	}
	if raw.ReadTimeout != "" {
		if c.ReadTimeout, err = time.ParseDuration(raw.ReadTimeout); err != nil {
			return fmt.Errorf("invalid read_timeout: %w", err)
		}
	}
	if raw.WriteTimeout != "" {
		if c.WriteTimeout, err = time.ParseDuration(raw.WriteTimeout); err != nil {
			return fmt.Errorf("invalid write_timeout: %w", err)
		}
	}
	if raw.PoolTimeout != "" {
		if c.PoolTimeout, err = time.ParseDuration(raw.PoolTimeout); err != nil {
			return fmt.Errorf("invalid pool_timeout: %w", err)
		}
	}
	if raw.ConnMaxIdleTime != "" {
		if c.ConnMaxIdleTime, err = time.ParseDuration(raw.ConnMaxIdleTime); err != nil {
			return fmt.Errorf("invalid conn_max_idle_time: %w", err)
		}
	}
	if raw.ConnMaxLifetime != "" {
		if c.ConnMaxLifetime, err = time.ParseDuration(raw.ConnMaxLifetime); err != nil {
			return fmt.Errorf("invalid conn_max_lifetime: %w", err)
		}
	}

	return nil
}

// MarshalJSON implements custom JSON marshaling for duration fields.
func (c Config) MarshalJSON() ([]byte, error) {
	raw := configJSON{
		Addrs:                 c.Addrs,
		MasterName:            c.MasterName,
		ClientName:            c.ClientName,
		Protocol:              c.Protocol,
		Username:              c.Username,
		Password:              c.Password,
		SentinelUsername:      c.SentinelUsername,
		SentinelPassword:      c.SentinelPassword,
		DB:                    c.DB,
		MaxRetries:            c.MaxRetries,
		ContextTimeoutEnabled: c.ContextTimeoutEnabled,
		PoolSize:              c.PoolSize,
		MinIdleConns:          c.MinIdleConns,
		MaxIdleConns:          c.MaxIdleConns,
		MaxActiveConns:        c.MaxActiveConns,
		PoolFIFO:              c.PoolFIFO,
		MaxRedirects:          c.MaxRedirects,
		IsClusterMode:         c.IsClusterMode,
		TLS:                   c.TLS,
		Lifecycle:             c.Lifecycle,
	}

	if c.MinRetryBackoff != 0 {
		raw.MinRetryBackoff = c.MinRetryBackoff.String()
	}
	if c.MaxRetryBackoff != 0 {
		raw.MaxRetryBackoff = c.MaxRetryBackoff.String()
	}
	if c.DialTimeout != 0 {
		raw.DialTimeout = c.DialTimeout.String()
	}
	if c.ReadTimeout != 0 {
		raw.ReadTimeout = c.ReadTimeout.String()
	}
	if c.WriteTimeout != 0 {
		raw.WriteTimeout = c.WriteTimeout.String()
	}
	if c.PoolTimeout != 0 {
		raw.PoolTimeout = c.PoolTimeout.String()
	}
	if c.ConnMaxIdleTime != 0 {
		raw.ConnMaxIdleTime = c.ConnMaxIdleTime.String()
	}
	if c.ConnMaxLifetime != 0 {
		raw.ConnMaxLifetime = c.ConnMaxLifetime.String()
	}

	return json.Marshal(raw)
}
