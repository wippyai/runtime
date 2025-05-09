// factory.go changes

package client

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/temporal"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ClientFactory is an interface for creating client instances
type ClientFactory interface {
	// CreateClient creates a new client instance
	CreateClient(logger *zap.Logger, id registry.ID, dc converter.DataConverter, cfg *api.ClientConfig) (*Client, error)
}

// DefaultClientFactory is the standard implementation of ClientFactory
type DefaultClientFactory struct{}

// NewDefaultClientFactory creates a new DefaultClientFactory
func NewDefaultClientFactory() *DefaultClientFactory {
	return &DefaultClientFactory{}
}

// APIKeyHeadersProvider implements the client.HeadersProvider interface for API key authentication
type APIKeyHeadersProvider struct {
	apiKey    string
	namespace string
}

// GetHeaders returns headers with API key authorization and namespace
func (p *APIKeyHeadersProvider) GetHeaders(ctx context.Context) (map[string]string, error) {
	return map[string]string{
		"Authorization":      "Bearer " + p.apiKey,
		"temporal-namespace": p.namespace,
	}, nil
}

// CreateClient implements ClientFactory interface
func (f *DefaultClientFactory) CreateClient(
	logger *zap.Logger,
	id registry.ID,
	dc converter.DataConverter,
	cfg *api.ClientConfig,
) (*Client, error) {
	// Create an instance with the configuration
	svc := &Client{
		id:             id,
		log:            logger.With(zap.String("client", id.String())),
		config:         cfg,
		dc:             dc,
		tqPrefix:       cfg.TQPrefix,
		statusChan:     make(chan any, 3), // Buffer for status updates
		exit:           make(chan struct{}),
		healthInterval: cfg.HealthCheck.Interval,
		healthEnabled:  cfg.HealthCheck.Enabled,
	}

	return svc, nil
}

// BuildClientOptions creates temporal client options from our configuration
func BuildClientOptions(
	cfg *api.ClientConfig,
	logger *zap.Logger,
	dc converter.DataConverter,
) (client.Options, error) {
	// Create client options
	options := client.Options{
		HostPort:      cfg.Connect.Address,
		Namespace:     cfg.Connect.Namespace,
		Logger:        newZapLogger(logger),
		DataConverter: dc,
	}

	// Configure authentication based on type
	if cfg.Auth.Type == api.AuthTypeAPIKey {
		apiKey, err := resolveAPIKey(cfg.Auth)
		if err != nil {
			return options, fmt.Errorf("failed to resolve API key: %w", err)
		}

		// Check if we can use the new API key credentials method (SDK v1.26.0+)
		if _, ok := interface{}(client.Options{}).(interface{ SetAPIKey(string) }); ok {
			// New SDK version supports direct API key credentials
			options.Credentials = client.NewAPIKeyStaticCredentials(apiKey)

			// Add the namespace header via gRPC interceptor
			options.ConnectionOptions.DialOptions = append(
				options.ConnectionOptions.DialOptions,
				grpc.WithUnaryInterceptor(
					func(ctx context.Context, method string, req any, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
						return invoker(
							metadata.AppendToOutgoingContext(ctx, "temporal-namespace", cfg.Connect.Namespace),
							method,
							req,
							reply,
							cc,
							opts...,
						)
					},
				),
			)
		} else {
			// Older SDK version - use headers provider with both headers
			options.HeadersProvider = &APIKeyHeadersProvider{
				apiKey:    apiKey,
				namespace: cfg.Connect.Namespace,
			}
		}
	} else if cfg.Auth.Type == api.AuthTypeTLS {
		// TLS authentication would be configured here
		logger.Warn("TLS authentication is not fully implemented yet")
	}

	return options, nil
}

// resolveAPIKey gets the API key either from config or file
func resolveAPIKey(auth api.AuthConfig) (string, error) {
	if auth.Type != api.AuthTypeAPIKey {
		return "", nil
	}

	// If API key is directly specified in config, use it
	if auth.APIKey != "" {
		// Check if it's an environment variable reference
		if strings.HasPrefix(auth.APIKey, "${") && strings.HasSuffix(auth.APIKey, "}") {
			// Extract environment variable name
			envVar := auth.APIKey[2 : len(auth.APIKey)-1]
			value := os.Getenv(envVar)
			if value == "" {
				return "", fmt.Errorf("environment variable %s for API key is not set", envVar)
			}
			return value, nil
		}
		return auth.APIKey, nil
	}

	// If key file is specified, read from file
	if auth.KeyFile != "" {
		content, err := os.ReadFile(auth.KeyFile)
		if err != nil {
			return "", fmt.Errorf("failed to read API key file: %w", err)
		}
		return strings.TrimSpace(string(content)), nil
	}

	return "", fmt.Errorf("no API key or key file specified")
}
