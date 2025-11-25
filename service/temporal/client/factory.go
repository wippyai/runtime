package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/temporal"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// ClientFactory creates Temporal clients from configuration
type ClientFactory interface {
	CreateClient(ctx context.Context, logger *zap.Logger, id registry.ID, config *api.ClientConfig) (*Client, error)
}

// DefaultClientFactory implements ClientFactory
type DefaultClientFactory struct {
	env                env.Registry
	dataConverter      converter.DataConverter
	clientInterceptors []interceptor.ClientInterceptor
}

// NewDefaultClientFactory creates a new default factory
func NewDefaultClientFactory(
	env env.Registry,
	dataConverter converter.DataConverter,
	clientInterceptors []interceptor.ClientInterceptor,
) *DefaultClientFactory {
	return &DefaultClientFactory{
		env:                env,
		dataConverter:      dataConverter,
		clientInterceptors: clientInterceptors,
	}
}

// CreateClient creates a new Temporal client from configuration
func (f *DefaultClientFactory) CreateClient(ctx context.Context, logger *zap.Logger, id registry.ID, config *api.ClientConfig) (*Client, error) {
	// Build client options
	opts, err := f.buildClientOptions(ctx, logger, config)
	if err != nil {
		return nil, fmt.Errorf("failed to build client options: %w", err)
	}

	// Apply connection timeout to context
	dialCtx := ctx
	if config.ConnectionTimeout > 0 {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, config.ConnectionTimeout)
		defer cancel()
	}

	// Create Temporal SDK client with timeout context
	temporalClient, err := client.DialContext(dialCtx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to temporal: %w", err)
	}

	// Wrap in our Client
	return NewClient(id, logger, temporalClient, config), nil
}

// buildClientOptions constructs Temporal client options from config
func (f *DefaultClientFactory) buildClientOptions(ctx context.Context, logger *zap.Logger, config *api.ClientConfig) (client.Options, error) {
	opts := client.Options{
		HostPort:  config.Address,
		Namespace: config.Namespace,
		Logger:    NewZapAdapter(logger),
	}

	// Set data converter if available
	if f.dataConverter != nil {
		opts.DataConverter = f.dataConverter
	}

	// Set client interceptors if available
	if len(f.clientInterceptors) > 0 {
		opts.Interceptors = f.clientInterceptors
	}

	// Configure authentication
	if err := f.configureAuth(ctx, config, &opts); err != nil {
		return opts, fmt.Errorf("failed to configure auth: %w", err)
	}

	// Configure TLS
	if err := f.configureTLS(config, &opts); err != nil {
		return opts, fmt.Errorf("failed to configure TLS: %w", err)
	}

	// Configure connection options
	if err := f.configureConnectionOptions(config, &opts); err != nil {
		return opts, fmt.Errorf("failed to configure connection options: %w", err)
	}

	return opts, nil
}

// configureAuth sets up authentication credentials
func (f *DefaultClientFactory) configureAuth(ctx context.Context, config *api.ClientConfig, opts *client.Options) error {
	switch config.Auth.Type {
	case api.AuthTypeNone:
		// No credentials needed
		return nil

	case api.AuthTypeAPIKey:
		apiKey, err := f.resolveAPIKey(ctx, config.Auth)
		if err != nil {
			return fmt.Errorf("failed to resolve API key: %w", err)
		}
		opts.Credentials = client.NewAPIKeyStaticCredentials(apiKey)
		return nil

	case api.AuthTypeMTLS:
		cert, err := f.loadClientCertificate(ctx, config.Auth)
		if err != nil {
			return fmt.Errorf("failed to load client certificate: %w", err)
		}
		opts.Credentials = client.NewMTLSCredentials(cert)
		return nil

	default:
		return fmt.Errorf("unsupported auth type: %s", config.Auth.Type)
	}
}

// resolveAPIKey resolves the API key from various sources
func (f *DefaultClientFactory) resolveAPIKey(ctx context.Context, auth api.AuthConfig) (string, error) {
	// Direct value
	if auth.APIKey != "" {
		return auth.APIKey, nil
	}

	// Environment variable
	if auth.APIKeyEnv != "" {
		if f.env == nil {
			return "", fmt.Errorf("env registry not available for api_key_env resolution")
		}
		apiKey, err := f.env.Get(ctx, auth.APIKeyEnv)
		if err != nil {
			return "", fmt.Errorf("failed to get api_key from env %s: %w", auth.APIKeyEnv, err)
		}
		return apiKey, nil
	}

	// File
	if auth.APIKeyFile != "" {
		data, err := os.ReadFile(auth.APIKeyFile)
		if err != nil {
			return "", fmt.Errorf("failed to read api_key_file %s: %w", auth.APIKeyFile, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	return "", fmt.Errorf("no API key source configured")
}

// loadClientCertificate loads the client certificate for mTLS
func (f *DefaultClientFactory) loadClientCertificate(ctx context.Context, auth api.AuthConfig) (tls.Certificate, error) {
	var certPEM, keyPEM []byte
	var err error

	// Load certificate
	if auth.CertFile != "" {
		certPEM, err = os.ReadFile(auth.CertFile)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("failed to read cert_file %s: %w", auth.CertFile, err)
		}
	} else if auth.CertPEM != "" {
		certPEM = []byte(auth.CertPEM)
	} else {
		return tls.Certificate{}, fmt.Errorf("no certificate source configured")
	}

	// Load private key
	if auth.KeyFile != "" {
		keyPEM, err = os.ReadFile(auth.KeyFile)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("failed to read key_file %s: %w", auth.KeyFile, err)
		}
	} else if auth.KeyPEM != "" {
		keyPEM = []byte(auth.KeyPEM)
	} else if auth.KeyPEMEnv != "" {
		if f.env == nil {
			return tls.Certificate{}, fmt.Errorf("env registry not available for key_pem_env resolution")
		}
		keyStr, err := f.env.Get(ctx, auth.KeyPEMEnv)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("failed to get key_pem from env %s: %w", auth.KeyPEMEnv, err)
		}
		keyPEM = []byte(keyStr)
	} else {
		return tls.Certificate{}, fmt.Errorf("no private key source configured")
	}

	// Parse certificate and key
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to parse certificate and key: %w", err)
	}

	return cert, nil
}

// configureTLS sets up TLS configuration
func (f *DefaultClientFactory) configureTLS(config *api.ClientConfig, opts *client.Options) error {
	if config.TLS == nil || !config.TLS.Enabled {
		return nil
	}

	tlsConfig := &tls.Config{
		ServerName:         config.TLS.ServerName,
		InsecureSkipVerify: config.TLS.InsecureSkipVerify,
	}

	// Load CA certificate if specified
	if config.TLS.CAFile != "" {
		caCert, err := os.ReadFile(config.TLS.CAFile)
		if err != nil {
			return fmt.Errorf("failed to read ca_file %s: %w", config.TLS.CAFile, err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return fmt.Errorf("failed to parse CA certificate from %s", config.TLS.CAFile)
		}
		tlsConfig.RootCAs = caCertPool
	}

	opts.ConnectionOptions = client.ConnectionOptions{
		TLS: tlsConfig,
	}

	return nil
}

// configureConnectionOptions sets up connection-related options
func (f *DefaultClientFactory) configureConnectionOptions(config *api.ClientConfig, opts *client.Options) error {
	// Set connection timeout if configured
	if config.ConnectionTimeout > 0 {
		// Add gRPC dial options for connection timeout
		dialOpts := []grpc.DialOption{
			grpc.WithBlock(),
		}

		if opts.ConnectionOptions.DialOptions == nil {
			opts.ConnectionOptions.DialOptions = dialOpts
		} else {
			opts.ConnectionOptions.DialOptions = append(opts.ConnectionOptions.DialOptions, dialOpts...)
		}
	}

	// Keep-alive settings are handled by gRPC automatically
	// We could add custom keep-alive params here if needed in the future

	return nil
}
