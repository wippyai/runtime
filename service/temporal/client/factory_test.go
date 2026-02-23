// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	temporalerrors "github.com/wippyai/runtime/service/temporal/errors"
	syspayload "github.com/wippyai/runtime/system/payload"
	msgpayload "github.com/wippyai/runtime/system/payload/msgpack"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestNewDefaultClientFactory(t *testing.T) {
	env := &mockEnvRegistry{values: make(map[string]string)}
	factory := NewDefaultClientFactory(env, nil, nil)

	require.NotNil(t, factory)
	assert.Equal(t, env, factory.env)
	assert.Nil(t, factory.dataConverter)
	assert.Nil(t, factory.clientInterceptors)
}

func TestDefaultClientFactory_buildClientOptions(t *testing.T) {
	t.Run("basic options", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, newTestDataConverterProvider(), nil)
		config := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "default",
			Auth:      api.AuthConfig{Type: api.AuthTypeNone},
		}

		opts, err := factory.buildClientOptions(context.Background(), zap.NewNop(), config)

		require.NoError(t, err)
		assert.Equal(t, "localhost:7233", opts.HostPort)
		assert.Equal(t, "default", opts.Namespace)
		assert.NotNil(t, opts.Logger)
		assert.Len(t, opts.ContextPropagators, 1)
		assert.NotNil(t, opts.FailureConverter)
		assert.NotEmpty(t, opts.ConnectionOptions.DialOptions)
	})

	t.Run("with data converter", func(t *testing.T) {
		dc := &mockDataConverter{}
		factory := NewDefaultClientFactory(nil, func() converter.DataConverter { return dc }, nil)
		config := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "test",
			Auth:      api.AuthConfig{Type: api.AuthTypeNone},
		}

		opts, err := factory.buildClientOptions(context.Background(), zap.NewNop(), config)

		require.NoError(t, err)
		assert.Equal(t, dc, opts.DataConverter)
		f := opts.FailureConverter.ErrorToFailure(errors.New("x"))
		require.NotNil(t, f)
		assert.Equal(t, temporalerrors.FailureSource, f.Source)
	})

	t.Run("with interceptors", func(t *testing.T) {
		interceptors := []interceptor.ClientInterceptor{&mockClientInterceptor{}}
		factory := NewDefaultClientFactory(nil, newTestDataConverterProvider(), interceptors)
		config := &api.ClientConfig{
			Address:   "localhost:7233",
			Namespace: "test",
			Auth:      api.AuthConfig{Type: api.AuthTypeNone},
		}

		opts, err := factory.buildClientOptions(context.Background(), zap.NewNop(), config)

		require.NoError(t, err)
		assert.Len(t, opts.Interceptors, 1)
	})
}

func newTestDataConverterProvider() func() converter.DataConverter {
	return func() converter.DataConverter {
		dtt := syspayload.NewTranscoder()
		msgpayload.Register(dtt)
		return dataconverter.NewDataConverter(dtt)
	}
}

func TestDefaultClientFactory_configureAuth(t *testing.T) {
	t.Run("no auth", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			Auth: api.AuthConfig{Type: api.AuthTypeNone},
		}
		opts := &client.Options{}

		err := factory.configureAuth(context.Background(), config, opts)

		require.NoError(t, err)
		assert.Nil(t, opts.Credentials)
	})

	t.Run("api key direct", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			Auth: api.AuthConfig{
				Type:   api.AuthTypeAPIKey,
				APIKey: "test-api-key",
			},
		}
		opts := &client.Options{}

		err := factory.configureAuth(context.Background(), config, opts)

		require.NoError(t, err)
		assert.NotNil(t, opts.Credentials)
	})

	t.Run("api key from env", func(t *testing.T) {
		env := &mockEnvRegistry{values: map[string]string{"TEMPORAL_API_KEY": "env-key"}}
		factory := NewDefaultClientFactory(env, nil, nil)
		config := &api.ClientConfig{
			Auth: api.AuthConfig{
				Type:      api.AuthTypeAPIKey,
				APIKeyEnv: "TEMPORAL_API_KEY",
			},
		}
		opts := &client.Options{}

		err := factory.configureAuth(context.Background(), config, opts)

		require.NoError(t, err)
		assert.NotNil(t, opts.Credentials)
	})

	t.Run("api key from env without registry fails", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			Auth: api.AuthConfig{
				Type:      api.AuthTypeAPIKey,
				APIKeyEnv: "TEMPORAL_API_KEY",
			},
		}
		opts := &client.Options{}

		err := factory.configureAuth(context.Background(), config, opts)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "env registry not available")
	})

	t.Run("api key from file", func(t *testing.T) {
		// Create temp file with API key
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "api-key")
		err := os.WriteFile(keyFile, []byte("file-api-key\n"), 0600)
		require.NoError(t, err)

		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			Auth: api.AuthConfig{
				Type:       api.AuthTypeAPIKey,
				APIKeyFile: keyFile,
			},
		}
		opts := &client.Options{}

		err = factory.configureAuth(context.Background(), config, opts)

		require.NoError(t, err)
		assert.NotNil(t, opts.Credentials)
	})

	t.Run("api key from missing file fails", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			Auth: api.AuthConfig{
				Type:       api.AuthTypeAPIKey,
				APIKeyFile: "/nonexistent/file",
			},
		}
		opts := &client.Options{}

		err := factory.configureAuth(context.Background(), config, opts)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read api_key_file")
	})

	t.Run("api key no source fails", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			Auth: api.AuthConfig{
				Type: api.AuthTypeAPIKey,
			},
		}
		opts := &client.Options{}

		err := factory.configureAuth(context.Background(), config, opts)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no API key source configured")
	})

	t.Run("unsupported auth type fails", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			Auth: api.AuthConfig{
				Type: "unknown",
			},
		}
		opts := &client.Options{}

		err := factory.configureAuth(context.Background(), config, opts)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported auth type")
	})
}

func TestDefaultClientFactory_loadClientCertificate(t *testing.T) {
	t.Run("missing cert source fails", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		auth := api.AuthConfig{
			KeyPEM: "some-key",
		}

		_, err := factory.loadClientCertificate(context.Background(), auth)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no certificate source configured")
	})

	t.Run("missing key source fails", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		auth := api.AuthConfig{
			CertPEM: "some-cert",
		}

		_, err := factory.loadClientCertificate(context.Background(), auth)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no private key source configured")
	})

	t.Run("key from env without registry fails", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		auth := api.AuthConfig{
			CertPEM:   "some-cert",
			KeyPEMEnv: "TEMPORAL_KEY",
		}

		_, err := factory.loadClientCertificate(context.Background(), auth)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "env registry not available")
	})

	t.Run("missing cert file fails", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		auth := api.AuthConfig{
			CertFile: "/nonexistent/cert.pem",
			KeyPEM:   "some-key",
		}

		_, err := factory.loadClientCertificate(context.Background(), auth)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read cert_file")
	})

	t.Run("missing key file fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		certFile := filepath.Join(tmpDir, "cert.pem")
		require.NoError(t, os.WriteFile(certFile, []byte("test-cert"), 0600))

		factory := NewDefaultClientFactory(nil, nil, nil)
		auth := api.AuthConfig{
			CertFile: certFile,
			KeyFile:  "/nonexistent/key.pem",
		}

		_, err := factory.loadClientCertificate(context.Background(), auth)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read key_file")
	})

	t.Run("invalid certificate fails", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		auth := api.AuthConfig{
			CertPEM: "invalid-cert",
			KeyPEM:  "invalid-key",
		}

		_, err := factory.loadClientCertificate(context.Background(), auth)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse certificate and key")
	})
}

func TestDefaultClientFactory_configureTLS(t *testing.T) {
	t.Run("TLS disabled", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			TLS: nil,
		}
		opts := &client.Options{}

		err := factory.configureTLS(zap.NewNop(), config, opts)

		require.NoError(t, err)
	})

	t.Run("TLS enabled but not active", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			TLS: &api.TLSConfig{Enabled: false},
		}
		opts := &client.Options{}

		err := factory.configureTLS(zap.NewNop(), config, opts)

		require.NoError(t, err)
	})

	t.Run("TLS with server name", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			TLS: &api.TLSConfig{
				Enabled:    true,
				ServerName: "temporal.example.com",
			},
		}
		opts := &client.Options{}

		err := factory.configureTLS(zap.NewNop(), config, opts)

		require.NoError(t, err)
		assert.NotNil(t, opts.ConnectionOptions.TLS)
		assert.Equal(t, "temporal.example.com", opts.ConnectionOptions.TLS.ServerName)
	})

	t.Run("TLS insecure skip verify", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			TLS: &api.TLSConfig{
				Enabled:            true,
				InsecureSkipVerify: true,
			},
		}
		opts := &client.Options{}

		err := factory.configureTLS(zap.NewNop(), config, opts)

		require.NoError(t, err)
		assert.True(t, opts.ConnectionOptions.TLS.InsecureSkipVerify)
	})

	t.Run("TLS with missing CA file fails", func(t *testing.T) {
		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			TLS: &api.TLSConfig{
				Enabled: true,
				CAFile:  "/nonexistent/ca.pem",
			},
		}
		opts := &client.Options{}

		err := factory.configureTLS(zap.NewNop(), config, opts)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read ca_file")
	})

	t.Run("TLS with invalid CA cert fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		caFile := filepath.Join(tmpDir, "ca.pem")
		require.NoError(t, os.WriteFile(caFile, []byte("invalid-ca"), 0600))

		factory := NewDefaultClientFactory(nil, nil, nil)
		config := &api.ClientConfig{
			TLS: &api.TLSConfig{
				Enabled: true,
				CAFile:  caFile,
			},
		}
		opts := &client.Options{}

		err := factory.configureTLS(zap.NewNop(), config, opts)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse CA certificate")
	})
}

func TestRewriteClientHeadersInterceptor(t *testing.T) {
	interceptor := rewriteClientHeadersInterceptor("wippy-go", "1.2.3")

	var captured metadata.MD
	invoker := func(
		ctx context.Context,
		_ string,
		_ any,
		_ any,
		_ *grpc.ClientConn,
		_ ...grpc.CallOption,
	) error {
		md, ok := metadata.FromOutgoingContext(ctx)
		require.True(t, ok)
		captured = md
		return nil
	}

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("foo", "bar"))
	err := interceptor(ctx, "/svc/method", nil, nil, nil, invoker)
	require.NoError(t, err)

	assert.Equal(t, "bar", captured.Get("foo")[0])
	assert.Equal(t, "wippy-go", captured.Get(temporalClientNameHeader)[0])
	assert.Equal(t, "1.2.3", captured.Get(temporalClientVersionHeader)[0])
}

func TestConfigureTransportHeaders_AppendsInterceptor(t *testing.T) {
	factory := NewDefaultClientFactory(nil, nil, nil)
	opts := &client.Options{}

	factory.configureTransportHeaders(opts)

	assert.NotEmpty(t, opts.ConnectionOptions.DialOptions)
}

// mockDataConverter for testing
type mockDataConverter struct{}

func (m *mockDataConverter) ToPayloads(_ ...any) (*commonpb.Payloads, error) {
	return nil, nil
}
func (m *mockDataConverter) FromPayloads(_ *commonpb.Payloads, _ ...any) error {
	return nil
}
func (m *mockDataConverter) ToPayload(_ any) (*commonpb.Payload, error) { return nil, nil }
func (m *mockDataConverter) FromPayload(_ *commonpb.Payload, _ any) error {
	return nil
}
func (m *mockDataConverter) ToString(_ *commonpb.Payload) string     { return "" }
func (m *mockDataConverter) ToStrings(_ *commonpb.Payloads) []string { return nil }

// mockClientInterceptor for testing
type mockClientInterceptor struct {
	interceptor.ClientInterceptorBase
}
