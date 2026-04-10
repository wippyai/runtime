// SPDX-License-Identifier: MPL-2.0

package redis

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Kind identifies the Redis Streams queue driver.
const Kind registry.Kind = "queue.driver.redis"

// Defaults for pending message recovery (XAUTOCLAIM).
const (
	// DefaultClaimInterval is how often the XAUTOCLAIM loop runs.
	DefaultClaimInterval = 30 * time.Second
	// DefaultClaimMinIdle is the minimum idle time before a pending message
	// can be auto-claimed from a crashed consumer.
	DefaultClaimMinIdle = time.Minute
)

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
	Addrs []string `json:"addrs,omitempty"`
	// TLS configures TLS/SSL connection settings.
	TLS *TLSConfig `json:"tls,omitempty"`

	// MasterName is the sentinel master name (enables sentinel/failover mode).
	MasterName string `json:"master_name,omitempty"`
	// ClientName executes CLIENT SETNAME for each connection.
	ClientName string `json:"client_name,omitempty"`
	// Username for ACL-based authentication (Redis 6.0+).
	Username string `json:"username,omitempty"`
	// Password for authentication (requirepass or ACL).
	Password string `json:"password,omitempty"`
	// SentinelUsername for ACL-based authentication with sentinel nodes.
	SentinelUsername string `json:"sentinel_username,omitempty"`
	// SentinelPassword for authentication with sentinel nodes.
	SentinelPassword string `json:"sentinel_password,omitempty"`

	MinRetryBackoff time.Duration `json:"min_retry_backoff,omitzero,format:units"`  // Default: 8ms, -1 disables
	MaxRetryBackoff time.Duration `json:"max_retry_backoff,omitzero,format:units"`  // Default: 512ms, -1 disables
	DialTimeout     time.Duration `json:"dial_timeout,omitzero,format:units"`       // Default: 5s
	ReadTimeout     time.Duration `json:"read_timeout,omitzero,format:units"`       // Default: 3s, -1 no timeout
	WriteTimeout    time.Duration `json:"write_timeout,omitzero,format:units"`      // Default: 3s, -1 no timeout
	PoolTimeout     time.Duration `json:"pool_timeout,omitzero,format:units"`       // Default: ReadTimeout + 1s
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time,omitzero,format:units"` // Default: 30m
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime,omitzero,format:units"`  // 0 = no limit

	// ClaimInterval is how often the XAUTOCLAIM loop runs to recover pending
	// messages from crashed consumers. Default: 30s. Set to 0 to disable.
	ClaimInterval time.Duration `json:"claim_interval,omitzero,format:units"`
	// ClaimMinIdle is the minimum idle time a pending message must have before
	// it can be auto-claimed. Default: 1m.
	ClaimMinIdle time.Duration `json:"claim_min_idle,omitzero,format:units"`

	Protocol       int `json:"protocol,omitempty"`    // RESP version (2 or 3), default 3
	DB             int `json:"db,omitempty"`          // Database number (standalone/sentinel only)
	MaxRetries     int `json:"max_retries,omitempty"` // -1 disables, default 3
	PoolSize       int `json:"pool_size,omitempty"`   // Per node, default 10*GOMAXPROCS
	MinIdleConns   int `json:"min_idle_conns,omitempty"`
	MaxIdleConns   int `json:"max_idle_conns,omitempty"`
	MaxActiveConns int `json:"max_active_conns,omitempty"` // 0 = no limit
	MaxRedirects   int `json:"max_redirects,omitempty"`    // Cluster mode only, default 3

	ContextTimeoutEnabled bool `json:"context_timeout_enabled,omitempty"`
	PoolFIFO              bool `json:"pool_fifo,omitempty"`
	// IsClusterMode forces cluster mode even with a single address
	// (useful for AWS ElastiCache config endpoints).
	IsClusterMode bool `json:"is_cluster_mode,omitempty"`
}

// TLSConfig defines TLS connection settings for Redis.
type TLSConfig struct {
	ServerName         string `json:"server_name,omitempty"`
	CertFile           string `json:"cert_file,omitempty"`
	KeyFile            string `json:"key_file,omitempty"`
	CAFile             string `json:"ca_file,omitempty"`
	Enabled            bool   `json:"enabled"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
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

	if t.CertFile != "" && t.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("redis tls: load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

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
	if c.ClaimInterval == 0 {
		c.ClaimInterval = DefaultClaimInterval
	}
	if c.ClaimMinIdle == 0 {
		c.ClaimMinIdle = DefaultClaimMinIdle
	}
}

// ToUniversalOptions converts the config to a go-redis UniversalOptions.
// TLS is handled separately via BuildTLSConfig since it requires file I/O.
func (c *Config) ToUniversalOptions() *goredis.UniversalOptions {
	return &goredis.UniversalOptions{
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
		MinRetryBackoff:       c.MinRetryBackoff,
		MaxRetryBackoff:       c.MaxRetryBackoff,
		DialTimeout:           c.DialTimeout,
		ReadTimeout:           c.ReadTimeout,
		WriteTimeout:          c.WriteTimeout,
		ContextTimeoutEnabled: c.ContextTimeoutEnabled,
		PoolSize:              c.PoolSize,
		MinIdleConns:          c.MinIdleConns,
		MaxIdleConns:          c.MaxIdleConns,
		MaxActiveConns:        c.MaxActiveConns,
		PoolFIFO:              c.PoolFIFO,
		PoolTimeout:           c.PoolTimeout,
		ConnMaxIdleTime:       c.ConnMaxIdleTime,
		ConnMaxLifetime:       c.ConnMaxLifetime,
		MaxRedirects:          c.MaxRedirects,
		IsClusterMode:         c.IsClusterMode,
	}
}

// durationField maps a JSON string field to its target duration pointer.
type durationField struct {
	src  string
	dst  *time.Duration
	name string
}

// UnmarshalJSON implements custom JSON unmarshaling for duration fields
// and backward compatibility with the old "addr" field.
func (c *Config) UnmarshalJSON(data []byte) error {
	// First pass: unmarshal everything except duration fields.
	var raw struct { //nolint:govet // fieldalignment: anonymous struct for JSON unmarshaling, readability preferred
		Lifecycle        supervisor.LifecycleConfig `json:"lifecycle"`
		Addrs            []string                   `json:"addrs,omitempty"`
		TLS              *TLSConfig                 `json:"tls,omitempty"`
		Addr             string                     `json:"addr,omitempty"` // backward compat
		MasterName       string                     `json:"master_name,omitempty"`
		ClientName       string                     `json:"client_name,omitempty"`
		Username         string                     `json:"username,omitempty"`
		Password         string                     `json:"password,omitempty"`
		SentinelUsername string                     `json:"sentinel_username,omitempty"`
		SentinelPassword string                     `json:"sentinel_password,omitempty"`
		// Durations as strings
		MinRetryBackoff string `json:"min_retry_backoff,omitempty"`
		MaxRetryBackoff string `json:"max_retry_backoff,omitempty"`
		DialTimeout     string `json:"dial_timeout,omitempty"`
		ReadTimeout     string `json:"read_timeout,omitempty"`
		WriteTimeout    string `json:"write_timeout,omitempty"`
		PoolTimeout     string `json:"pool_timeout,omitempty"`
		ConnMaxIdleTime string `json:"conn_max_idle_time,omitempty"`
		ConnMaxLifetime string `json:"conn_max_lifetime,omitempty"`
		ClaimInterval   string `json:"claim_interval,omitempty"`
		ClaimMinIdle    string `json:"claim_min_idle,omitempty"`
		// Scalars
		Protocol              int  `json:"protocol,omitempty"`
		DB                    int  `json:"db,omitempty"`
		MaxRetries            int  `json:"max_retries,omitempty"`
		PoolSize              int  `json:"pool_size,omitempty"`
		MinIdleConns          int  `json:"min_idle_conns,omitempty"`
		MaxIdleConns          int  `json:"max_idle_conns,omitempty"`
		MaxActiveConns        int  `json:"max_active_conns,omitempty"`
		MaxRedirects          int  `json:"max_redirects,omitempty"`
		ContextTimeoutEnabled bool `json:"context_timeout_enabled,omitempty"`
		PoolFIFO              bool `json:"pool_fifo,omitempty"`
		IsClusterMode         bool `json:"is_cluster_mode,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Copy scalars
	c.Lifecycle = raw.Lifecycle
	c.Addrs = raw.Addrs
	c.TLS = raw.TLS
	c.MasterName = raw.MasterName
	c.ClientName = raw.ClientName
	c.Username = raw.Username
	c.Password = raw.Password
	c.SentinelUsername = raw.SentinelUsername
	c.SentinelPassword = raw.SentinelPassword
	c.Protocol = raw.Protocol
	c.DB = raw.DB
	c.MaxRetries = raw.MaxRetries
	c.PoolSize = raw.PoolSize
	c.MinIdleConns = raw.MinIdleConns
	c.MaxIdleConns = raw.MaxIdleConns
	c.MaxActiveConns = raw.MaxActiveConns
	c.MaxRedirects = raw.MaxRedirects
	c.ContextTimeoutEnabled = raw.ContextTimeoutEnabled
	c.PoolFIFO = raw.PoolFIFO
	c.IsClusterMode = raw.IsClusterMode

	// Backward compatibility: "addr" → "addrs"
	if len(c.Addrs) == 0 && raw.Addr != "" {
		c.Addrs = []string{raw.Addr}
	}

	// Parse duration fields via table
	for _, f := range []durationField{
		{raw.MinRetryBackoff, &c.MinRetryBackoff, "min_retry_backoff"},
		{raw.MaxRetryBackoff, &c.MaxRetryBackoff, "max_retry_backoff"},
		{raw.DialTimeout, &c.DialTimeout, "dial_timeout"},
		{raw.ReadTimeout, &c.ReadTimeout, "read_timeout"},
		{raw.WriteTimeout, &c.WriteTimeout, "write_timeout"},
		{raw.PoolTimeout, &c.PoolTimeout, "pool_timeout"},
		{raw.ConnMaxIdleTime, &c.ConnMaxIdleTime, "conn_max_idle_time"},
		{raw.ConnMaxLifetime, &c.ConnMaxLifetime, "conn_max_lifetime"},
		{raw.ClaimInterval, &c.ClaimInterval, "claim_interval"},
		{raw.ClaimMinIdle, &c.ClaimMinIdle, "claim_min_idle"},
	} {
		if f.src != "" {
			d, err := time.ParseDuration(f.src)
			if err != nil {
				return fmt.Errorf("invalid %s: %w", f.name, err)
			}
			*f.dst = d
		}
	}

	return nil
}

// MarshalJSON implements custom JSON marshaling for duration fields.
func (c Config) MarshalJSON() ([]byte, error) {
	type configAlias Config // avoid recursion
	raw := struct {         //nolint:govet // fieldalignment: anonymous struct for JSON marshaling, readability preferred
		configAlias
		MinRetryBackoff string `json:"min_retry_backoff,omitempty"`
		MaxRetryBackoff string `json:"max_retry_backoff,omitempty"`
		DialTimeout     string `json:"dial_timeout,omitempty"`
		ReadTimeout     string `json:"read_timeout,omitempty"`
		WriteTimeout    string `json:"write_timeout,omitempty"`
		PoolTimeout     string `json:"pool_timeout,omitempty"`
		ConnMaxIdleTime string `json:"conn_max_idle_time,omitempty"`
		ConnMaxLifetime string `json:"conn_max_lifetime,omitempty"`
		ClaimInterval   string `json:"claim_interval,omitempty"`
		ClaimMinIdle    string `json:"claim_min_idle,omitempty"`
	}{configAlias: configAlias(c)}

	// Override duration fields with string representations
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
	if c.ClaimInterval != 0 {
		raw.ClaimInterval = c.ClaimInterval.String()
	}
	if c.ClaimMinIdle != 0 {
		raw.ClaimMinIdle = c.ClaimMinIdle.String()
	}

	return json.Marshal(raw)
}
