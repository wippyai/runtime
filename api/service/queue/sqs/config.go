// SPDX-License-Identifier: MPL-2.0

package sqs

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Kind identifies the AWS SQS queue driver.
const Kind registry.Kind = "queue.driver.sqs"

// Config defines the AWS SQS queue driver configuration.
//
// AWS credentials are resolved in this order:
//  1. If AWSConfig is set, the shared config.aws resource is used.
//  2. If AccessKeyID/SecretAccessKey are set, static credentials are used.
//  3. Otherwise the SDK default credential chain is used (env, profile, IMDS).
//
// HTTP transport, TLS, retry, and SQS-specific options are applied on top.
type Config struct { //nolint:govet // fieldalignment: limited by LifecycleConfig embedded struct layout
	// Lifecycle configures the supervisor lifecycle for this driver.
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`

	// TLS configures TLS settings for the HTTP transport.
	// When TLS.Enabled is true, a custom TLS configuration is applied.
	TLS *TLSConfig `json:"tls,omitempty"`

	// AWSConfig is an optional reference to a shared config.aws resource ID.
	// When set, credentials and region are acquired from the shared resource
	// (same pattern as the S3 driver). Inline Region/AccessKeyID fields are
	// ignored when this is set.
	AWSConfig string `json:"aws_config,omitempty"`

	// Region is the AWS region (e.g. "us-east-1").
	// Ignored when AWSConfig is set.
	Region string `json:"region,omitempty"`

	// Endpoint is a custom endpoint URL (e.g. for LocalStack, ElasticMQ).
	// Sets BaseEndpoint on the AWS config.
	Endpoint string `json:"endpoint,omitempty"`

	// Profile is the AWS shared config profile name.
	// Ignored when AWSConfig is set.
	Profile string `json:"profile,omitempty"`

	// AccessKeyID is the AWS access key for static credentials.
	// Ignored when AWSConfig is set.
	AccessKeyID string `json:"access_key_id,omitempty"`

	// SecretAccessKey is the AWS secret key for static credentials.
	// Ignored when AWSConfig is set.
	SecretAccessKey string `json:"secret_access_key,omitempty"`

	// SessionToken is an optional session token for temporary credentials.
	// Ignored when AWSConfig is set.
	SessionToken string `json:"session_token,omitempty"`

	// RetryMode selects the retry strategy: "standard" or "adaptive".
	// "standard" uses exponential backoff with jitter.
	// "adaptive" adds client-side rate limiting on throttle errors.
	// Empty uses the SDK default ("standard").
	RetryMode string `json:"retry_mode,omitempty"`

	// HTTPClientTimeout is the overall HTTP client timeout.
	// 0 uses the SDK default.
	HTTPClientTimeout time.Duration `json:"http_client_timeout,omitzero,format:units"`

	// ConnectTimeout is the TCP dial timeout for new connections.
	// 0 uses the SDK default (30s).
	ConnectTimeout time.Duration `json:"connect_timeout,omitzero,format:units"`

	// TLSHandshakeTimeout is the TLS negotiation timeout.
	// 0 uses the SDK default (10s).
	TLSHandshakeTimeout time.Duration `json:"tls_handshake_timeout,omitzero,format:units"`

	// IdleConnTimeout is the maximum time an idle connection remains in the pool.
	// 0 uses the SDK default (90s).
	IdleConnTimeout time.Duration `json:"idle_conn_timeout,omitzero,format:units"`

	// KeepAlive is the TCP keep-alive interval for connections.
	// 0 uses the SDK default (30s).
	KeepAlive time.Duration `json:"keep_alive,omitzero,format:units"`

	// MaxIdleConns is the maximum number of idle connections across all hosts.
	// 0 uses the SDK default (100).
	MaxIdleConns int `json:"max_idle_conns,omitempty"`

	// MaxIdleConnsPerHost is the maximum idle connections per host.
	// 0 uses the SDK default (10).
	MaxIdleConnsPerHost int `json:"max_idle_conns_per_host,omitempty"`

	// MaxConnsPerHost is the maximum total connections per host.
	// 0 uses the SDK default (2048).
	MaxConnsPerHost int `json:"max_conns_per_host,omitempty"`

	// RetryMaxAttempts is the maximum number of retry attempts.
	// 0 uses the SDK default (3).
	RetryMaxAttempts int `json:"retry_max_attempts,omitempty"`

	// MaxNumberOfMessages is the max messages per ReceiveMessage call (1–10).
	// Default: 10.
	MaxNumberOfMessages int32 `json:"max_number_of_messages,omitempty"`

	// WaitTimeSeconds is the long-poll wait time in seconds (0–20).
	// 0 means short polling. Default: 20.
	WaitTimeSeconds int32 `json:"wait_time_seconds,omitempty"`

	// VisibilityTimeout is the default visibility timeout in seconds (0–43200)
	// applied per ReceiveMessage call.
	// 0 uses the queue's default (typically 30s).
	VisibilityTimeout int32 `json:"visibility_timeout,omitempty"`

	// MessageRetentionPeriod is the queue-level message retention in seconds (60–1209600).
	// Applied as a queue attribute on CreateQueue.
	// 0 uses the AWS default (345600 = 4 days).
	MessageRetentionPeriod int32 `json:"message_retention_period,omitempty"`

	// DefaultDelaySeconds is the default delivery delay for messages (0–900).
	// Applied as a queue attribute on CreateQueue.
	// 0 means no delay.
	DefaultDelaySeconds int32 `json:"default_delay_seconds,omitempty"`

	// DisableMessageChecksumValidation disables SQS message checksum validation
	// for SendMessage, SendMessageBatch, and ReceiveMessage operations.
	DisableMessageChecksumValidation bool `json:"disable_message_checksum_validation,omitempty"`

	// UseFIPS enables FIPS-compliant endpoints.
	UseFIPS bool `json:"use_fips,omitempty"`

	// UseDualStack enables dual-stack (IPv4 + IPv6) endpoints.
	UseDualStack bool `json:"use_dual_stack,omitempty"`

	// DisableHTTP2 forces HTTP/1.1 instead of HTTP/2.
	DisableHTTP2 bool `json:"disable_http2,omitempty"`
}

// TLSConfig defines TLS settings for the SQS HTTP transport.
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

	// Enabled activates custom TLS configuration for the HTTP transport.
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
			return nil, fmt.Errorf("sqs tls: load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate
	if t.CAFile != "" {
		caCert, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("sqs tls: read ca file: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("sqs tls: failed to parse ca certificate")
		}
		tlsCfg.RootCAs = caCertPool
	}

	return tlsCfg, nil
}

// BuildHTTPClient constructs an AWS HTTP client from the transport/TLS config.
// Returns a zero-value BuildableClient and false when no custom settings are
// configured (SDK uses its defaults).
func (c *Config) BuildHTTPClient() (*awshttp.BuildableClient, bool, error) {
	hasTransport := c.ConnectTimeout != 0 || c.TLSHandshakeTimeout != 0 ||
		c.IdleConnTimeout != 0 || c.KeepAlive != 0 ||
		c.MaxIdleConns != 0 || c.MaxIdleConnsPerHost != 0 || c.MaxConnsPerHost != 0 ||
		c.DisableHTTP2
	hasTLS := c.TLS != nil && c.TLS.Enabled
	hasTimeout := c.HTTPClientTimeout != 0

	if !hasTransport && !hasTLS && !hasTimeout {
		return nil, false, nil
	}

	client := awshttp.NewBuildableClient()

	if hasTimeout {
		client = client.WithTimeout(c.HTTPClientTimeout)
	}

	// Dialer options
	if c.ConnectTimeout != 0 || c.KeepAlive != 0 {
		client = client.WithDialerOptions(func(d *net.Dialer) {
			if c.ConnectTimeout != 0 {
				d.Timeout = c.ConnectTimeout
			}
			if c.KeepAlive != 0 {
				d.KeepAlive = c.KeepAlive
			}
		})
	}

	// Transport options
	var tlsCfg *tls.Config
	if hasTLS {
		var err error
		tlsCfg, err = c.TLS.BuildTLSConfig()
		if err != nil {
			return nil, false, err
		}
	}

	if c.TLSHandshakeTimeout != 0 || c.IdleConnTimeout != 0 ||
		c.MaxIdleConns != 0 || c.MaxIdleConnsPerHost != 0 || c.MaxConnsPerHost != 0 ||
		c.DisableHTTP2 || tlsCfg != nil {
		client = client.WithTransportOptions(func(tr *http.Transport) {
			if c.TLSHandshakeTimeout != 0 {
				tr.TLSHandshakeTimeout = c.TLSHandshakeTimeout
			}
			if c.IdleConnTimeout != 0 {
				tr.IdleConnTimeout = c.IdleConnTimeout
			}
			if c.MaxIdleConns != 0 {
				tr.MaxIdleConns = c.MaxIdleConns
			}
			if c.MaxIdleConnsPerHost != 0 {
				tr.MaxIdleConnsPerHost = c.MaxIdleConnsPerHost
			}
			if c.MaxConnsPerHost != 0 {
				tr.MaxConnsPerHost = c.MaxConnsPerHost
			}
			if c.DisableHTTP2 {
				tr.ForceAttemptHTTP2 = false
			}
			if tlsCfg != nil {
				tr.TLSClientConfig = tlsCfg
			}
		})
	}

	return client, true, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.AWSConfig == "" && c.Region == "" {
		return fmt.Errorf("sqs: region is required when aws_config is not set")
	}
	if c.TLS != nil && c.TLS.CertFile != "" && c.TLS.KeyFile == "" {
		return fmt.Errorf("sqs: tls cert_file requires key_file")
	}
	if c.TLS != nil && c.TLS.KeyFile != "" && c.TLS.CertFile == "" {
		return fmt.Errorf("sqs: tls key_file requires cert_file")
	}
	switch c.RetryMode {
	case "", "standard", "adaptive":
		// valid
	default:
		return fmt.Errorf("sqs: unsupported retry_mode %q (supported: standard, adaptive)", c.RetryMode)
	}
	if c.MaxNumberOfMessages <= 0 || c.MaxNumberOfMessages > 10 {
		return fmt.Errorf("sqs: max_number_of_messages must be 1–10, got %d", c.MaxNumberOfMessages)
	}
	if c.WaitTimeSeconds < 0 || c.WaitTimeSeconds > 20 {
		return fmt.Errorf("sqs: wait_time_seconds must be 0–20, got %d", c.WaitTimeSeconds)
	}
	if c.VisibilityTimeout < 0 || c.VisibilityTimeout > 43200 {
		return fmt.Errorf("sqs: visibility_timeout must be 0–43200, got %d", c.VisibilityTimeout)
	}
	if c.MessageRetentionPeriod != 0 && (c.MessageRetentionPeriod < 60 || c.MessageRetentionPeriod > 1209600) {
		return fmt.Errorf("sqs: message_retention_period must be 60–1209600, got %d", c.MessageRetentionPeriod)
	}
	if c.DefaultDelaySeconds < 0 || c.DefaultDelaySeconds > 900 {
		return fmt.Errorf("sqs: default_delay_seconds must be 0–900, got %d", c.DefaultDelaySeconds)
	}
	return nil
}

// InitDefaults initializes default values.
func (c *Config) InitDefaults() {
	if c.MaxNumberOfMessages == 0 {
		c.MaxNumberOfMessages = 10
	}
	if c.WaitTimeSeconds == 0 {
		c.WaitTimeSeconds = 20
	}
}

// configJSON is a shadow struct for JSON marshaling/unmarshaling
// of duration fields and backward compatibility.
type configJSON struct { //nolint:govet // fieldalignment: limited by LifecycleConfig embedded struct layout
	Lifecycle                        supervisor.LifecycleConfig `json:"lifecycle"`
	TLS                              *TLSConfig                 `json:"tls,omitempty"`
	AWSConfig                        string                     `json:"aws_config,omitempty"`
	Region                           string                     `json:"region,omitempty"`
	Endpoint                         string                     `json:"endpoint,omitempty"`
	Profile                          string                     `json:"profile,omitempty"`
	AccessKeyID                      string                     `json:"access_key_id,omitempty"`
	SecretAccessKey                  string                     `json:"secret_access_key,omitempty"`
	SessionToken                     string                     `json:"session_token,omitempty"`
	RetryMode                        string                     `json:"retry_mode,omitempty"`
	HTTPClientTimeout                string                     `json:"http_client_timeout,omitempty"`
	ConnectTimeout                   string                     `json:"connect_timeout,omitempty"`
	TLSHandshakeTimeout              string                     `json:"tls_handshake_timeout,omitempty"`
	IdleConnTimeout                  string                     `json:"idle_conn_timeout,omitempty"`
	KeepAlive                        string                     `json:"keep_alive,omitempty"`
	MaxIdleConns                     int                        `json:"max_idle_conns,omitempty"`
	MaxIdleConnsPerHost              int                        `json:"max_idle_conns_per_host,omitempty"`
	MaxConnsPerHost                  int                        `json:"max_conns_per_host,omitempty"`
	RetryMaxAttempts                 int                        `json:"retry_max_attempts,omitempty"`
	MaxNumberOfMessages              int32                      `json:"max_number_of_messages,omitempty"`
	WaitTimeSeconds                  int32                      `json:"wait_time_seconds,omitempty"`
	VisibilityTimeout                int32                      `json:"visibility_timeout,omitempty"`
	MessageRetentionPeriod           int32                      `json:"message_retention_period,omitempty"`
	DefaultDelaySeconds              int32                      `json:"default_delay_seconds,omitempty"`
	DisableMessageChecksumValidation bool                       `json:"disable_message_checksum_validation,omitempty"`
	UseFIPS                          bool                       `json:"use_fips,omitempty"`
	UseDualStack                     bool                       `json:"use_dual_stack,omitempty"`
	DisableHTTP2                     bool                       `json:"disable_http2,omitempty"`
}

// UnmarshalJSON implements custom JSON unmarshaling for duration fields.
func (c *Config) UnmarshalJSON(data []byte) error {
	var raw configJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.AWSConfig = raw.AWSConfig
	c.Region = raw.Region
	c.Endpoint = raw.Endpoint
	c.Profile = raw.Profile
	c.AccessKeyID = raw.AccessKeyID
	c.SecretAccessKey = raw.SecretAccessKey
	c.SessionToken = raw.SessionToken
	c.RetryMode = raw.RetryMode
	c.MaxIdleConns = raw.MaxIdleConns
	c.MaxIdleConnsPerHost = raw.MaxIdleConnsPerHost
	c.MaxConnsPerHost = raw.MaxConnsPerHost
	c.RetryMaxAttempts = raw.RetryMaxAttempts
	c.MaxNumberOfMessages = raw.MaxNumberOfMessages
	c.WaitTimeSeconds = raw.WaitTimeSeconds
	c.VisibilityTimeout = raw.VisibilityTimeout
	c.MessageRetentionPeriod = raw.MessageRetentionPeriod
	c.DefaultDelaySeconds = raw.DefaultDelaySeconds
	c.DisableMessageChecksumValidation = raw.DisableMessageChecksumValidation
	c.UseFIPS = raw.UseFIPS
	c.UseDualStack = raw.UseDualStack
	c.DisableHTTP2 = raw.DisableHTTP2
	c.TLS = raw.TLS
	c.Lifecycle = raw.Lifecycle

	// Parse duration fields
	var err error
	if raw.HTTPClientTimeout != "" {
		if c.HTTPClientTimeout, err = time.ParseDuration(raw.HTTPClientTimeout); err != nil {
			return fmt.Errorf("invalid http_client_timeout: %w", err)
		}
	}
	if raw.ConnectTimeout != "" {
		if c.ConnectTimeout, err = time.ParseDuration(raw.ConnectTimeout); err != nil {
			return fmt.Errorf("invalid connect_timeout: %w", err)
		}
	}
	if raw.TLSHandshakeTimeout != "" {
		if c.TLSHandshakeTimeout, err = time.ParseDuration(raw.TLSHandshakeTimeout); err != nil {
			return fmt.Errorf("invalid tls_handshake_timeout: %w", err)
		}
	}
	if raw.IdleConnTimeout != "" {
		if c.IdleConnTimeout, err = time.ParseDuration(raw.IdleConnTimeout); err != nil {
			return fmt.Errorf("invalid idle_conn_timeout: %w", err)
		}
	}
	if raw.KeepAlive != "" {
		if c.KeepAlive, err = time.ParseDuration(raw.KeepAlive); err != nil {
			return fmt.Errorf("invalid keep_alive: %w", err)
		}
	}

	return nil
}

// MarshalJSON implements custom JSON marshaling for duration fields.
func (c Config) MarshalJSON() ([]byte, error) {
	raw := configJSON{
		AWSConfig:                        c.AWSConfig,
		Region:                           c.Region,
		Endpoint:                         c.Endpoint,
		Profile:                          c.Profile,
		AccessKeyID:                      c.AccessKeyID,
		SecretAccessKey:                  c.SecretAccessKey,
		SessionToken:                     c.SessionToken,
		RetryMode:                        c.RetryMode,
		MaxIdleConns:                     c.MaxIdleConns,
		MaxIdleConnsPerHost:              c.MaxIdleConnsPerHost,
		MaxConnsPerHost:                  c.MaxConnsPerHost,
		RetryMaxAttempts:                 c.RetryMaxAttempts,
		MaxNumberOfMessages:              c.MaxNumberOfMessages,
		WaitTimeSeconds:                  c.WaitTimeSeconds,
		VisibilityTimeout:                c.VisibilityTimeout,
		MessageRetentionPeriod:           c.MessageRetentionPeriod,
		DefaultDelaySeconds:              c.DefaultDelaySeconds,
		DisableMessageChecksumValidation: c.DisableMessageChecksumValidation,
		UseFIPS:                          c.UseFIPS,
		UseDualStack:                     c.UseDualStack,
		DisableHTTP2:                     c.DisableHTTP2,
		TLS:                              c.TLS,
		Lifecycle:                        c.Lifecycle,
	}

	if c.HTTPClientTimeout != 0 {
		raw.HTTPClientTimeout = c.HTTPClientTimeout.String()
	}
	if c.ConnectTimeout != 0 {
		raw.ConnectTimeout = c.ConnectTimeout.String()
	}
	if c.TLSHandshakeTimeout != 0 {
		raw.TLSHandshakeTimeout = c.TLSHandshakeTimeout.String()
	}
	if c.IdleConnTimeout != 0 {
		raw.IdleConnTimeout = c.IdleConnTimeout.String()
	}
	if c.KeepAlive != 0 {
		raw.KeepAlive = c.KeepAlive.String()
	}

	return json.Marshal(raw)
}
