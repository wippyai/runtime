// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authapi "github.com/wippyai/runtime/api/auth"
	"github.com/wippyai/runtime/api/boot"
	regapi "github.com/wippyai/runtime/api/registry"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	bootpkg "github.com/wippyai/runtime/boot"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestReadKindSlice_InvalidTypeDoesNotOverrideDefaults(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
		RegistryDispatchInternalKinds: 42,
	}))

	kinds, ok := readKindSlice(cfg.Sub(RegistryName), RegistryDispatchInternalKinds)
	assert.False(t, ok)
	assert.Nil(t, kinds)
}

func TestReadKindSlice_ValidList(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
		RegistryDispatchInternalKinds: []string{"registry.entry", "ns.dependency"},
	}))

	kinds, ok := readKindSlice(cfg.Sub(RegistryName), RegistryDispatchInternalKinds)
	assert.True(t, ok)
	assert.Equal(t, []string{"registry.entry", "ns.dependency"}, kinds)
}

func TestReadKindSlice_MixedAnyValues(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
		RegistryDispatchInternalKinds: []any{"registry.entry", 7, "ns.definition"},
	}))

	kinds, ok := readKindSlice(cfg.Sub(RegistryName), RegistryDispatchInternalKinds)
	assert.True(t, ok)
	assert.Equal(t, []string{"registry.entry", "ns.definition"}, kinds)
}

func TestCoreDependencyPatternsIncludeExplicitMetadataAndLifecycleRefs(t *testing.T) {
	patterns := append(getDefaultDependencyPatterns(), getLifecycleDependencyPatterns()...)
	paths := make(map[string]bool, len(patterns))
	for _, pattern := range patterns {
		paths[pattern.Path] = pattern.AllowWildcard
	}

	require.Contains(t, paths, "meta.depends_on")
	require.True(t, paths["meta.depends_on"])
	require.Contains(t, paths, "data.*.depends_on")
	require.True(t, paths["data.*.depends_on"])
	require.Contains(t, paths, "data.lifecycle.requires")
	require.True(t, paths["data.lifecycle.requires"])
	require.Contains(t, paths, "data.lifecycle.depends_on")
	require.True(t, paths["data.lifecycle.depends_on"])
}

func TestRegistryPostgresHistoryRequiresDSN(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
		RegistryEnableHistory: true,
		RegistryHistoryType:   "postgres",
	}))
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), cfg)
	require.NoError(t, err)

	loader, err := bootpkg.NewLoader(Artifacts(), Registry())
	require.NoError(t, err)

	_, err = loader.Load(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "history DSN is required")
}

func TestRegistryRemoteHistoryRequiresName(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{RegistryEnableHistory: true, RegistryHistoryType: "grpc"}))
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), cfg)
	require.NoError(t, err)
	loader, err := bootpkg.NewLoader(Artifacts(), Registry())
	require.NoError(t, err)
	_, err = loader.Load(ctx)
	require.ErrorContains(t, err, "history_registry_id is required")
}

func TestRegistryRemoteEndpointRequiresCredentials(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(bootauth.EnvToken, "")
	t.Setenv(bootauth.EnvRegistry, "https://history-endpoint-test.invalid")
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
		"history_endpoint":       "history.example.com:443",
		"history_environment_id": "test",
		"history_registry_id":    "app",
	}))
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), cfg)
	require.NoError(t, err)
	loader, err := bootpkg.NewLoader(Artifacts(), Registry())
	require.NoError(t, err)
	_, err = loader.Load(ctx)
	require.ErrorContains(t, err, "history authentication is required")
}

type emptyRemoteRegistryServer struct {
	historyv1.UnimplementedHistoryServiceServer
}

func (*emptyRemoteRegistryServer) GetVersion(_ context.Context, request *historyv1.GetRequest) (*historyv1.Version, error) {
	if request.Exact {
		return nil, status.Error(codes.NotFound, "history was not found")
	}
	return &historyv1.Version{}, nil
}

func TestRegistryRemoteFirstBoot(t *testing.T) {
	for _, source := range []string{"file", "environment", "login", "environment-over-login", "empty-type"} {
		t.Run(source, func(t *testing.T) { testRegistryRemoteFirstBoot(t, source) })
	}
}

func testRegistryRemoteFirstBoot(t *testing.T, source string) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv(bootauth.EnvToken, "")
	t.Setenv(bootauth.EnvRegistry, "https://history-auth-test.invalid")
	const token = "wpy_history_test_credential"
	certificateServer := httptest.NewTLSServer(nil)
	certificate := certificateServer.TLS.Certificates[0]
	certificateServer.Close()
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}})), grpc.UnaryInterceptor(func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		values := metadata.ValueFromIncomingContext(ctx, "authorization")
		if len(values) != 1 || values[0] != "Bearer "+token {
			return nil, status.Error(codes.Unauthenticated, "incorrect credential")
		}
		return handler(ctx, request)
	}))
	historyv1.RegisterHistoryServiceServer(server, &emptyRemoteRegistryServer{})
	go server.Serve(listener)
	defer server.Stop()
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600))
	settings := map[string]any{
		RegistryEnableHistory: true,
		"history_endpoint":    listener.Addr().String(), "history_ca_file": caFile,
		"history_tenant_id": "test", "history_environment_id": "test", "history_registry_id": "test",
	}
	switch source {
	case "file":
		settings[RegistryHistoryType] = "grpc"
		require.NoError(t, os.WriteFile(tokenFile, []byte(token), 0600))
		settings["history_token_file"] = tokenFile
		settings["history_replica_id"] = "test"
		settings["history_timeout"] = time.Second
		settings["history_poll_interval"] = time.Millisecond
		t.Setenv(bootauth.EnvToken, "wrong-environment-token")
	case "environment", "empty-type":
		t.Setenv(bootauth.EnvToken, token)
		if source == "empty-type" {
			settings[RegistryHistoryType] = ""
		}
	case "login", "environment-over-login":
		projectDir, err := os.Getwd()
		require.NoError(t, err)
		store := bootauth.NewStore(bootauth.NewConfig(projectDir))
		stored := token
		if source == "environment-over-login" {
			stored = "wrong-saved-token"
			t.Setenv(bootauth.EnvToken, token)
		}
		require.NoError(t, store.Set(&authapi.Credential{Token: stored, Registry: "https://history-auth-test.invalid"}, false))
	}
	cfg := boot.NewConfig(boot.WithSection(RegistryName, settings))
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), cfg)
	require.NoError(t, err)
	component := Registry()
	loader, err := bootpkg.NewLoader(Artifacts(), component)
	require.NoError(t, err)
	ctx, err = loader.Load(ctx)
	require.NoError(t, err)
	defer loader.Shutdown(ctx)
	history := regapi.GetRegistry(ctx).History()
	head, err := history.Head()
	require.NoError(t, err)
	require.Zero(t, head.ID())
	require.NoError(t, regapi.GetRegistry(ctx).LoadState(ctx, nil, head))
	require.NoError(t, loader.Start(ctx))
}

func TestRegistryHistoryNameRequiresCredentials(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(bootauth.EnvToken, "")
	t.Setenv(bootauth.EnvRegistry, "https://hub.history-name-test.invalid")
	cfg := boot.NewConfig(boot.WithSection(RegistryName, map[string]any{
		"history_registry_id": "app",
	}))
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), cfg)
	require.NoError(t, err)
	loader, err := bootpkg.NewLoader(Artifacts(), Registry())
	require.NoError(t, err)
	_, err = loader.Load(ctx)
	require.ErrorContains(t, err, "history authentication is required")
}
