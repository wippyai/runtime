// SPDX-License-Identifier: MPL-2.0

package tailscale

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	envapi "github.com/wippyai/runtime/api/env"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	netservice "github.com/wippyai/runtime/service/net"
)

// --- test doubles ---

type mockTranscoder struct {
	unmarshalFunc func(payload.Payload, any) error
}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func (m *mockTranscoder) Marshal(any) (payload.Payload, error) { return nil, errors.New("unused") }

func (m *mockTranscoder) Unmarshal(p payload.Payload, v any) error {
	if m.unmarshalFunc == nil {
		return errors.New("unmarshal not set")
	}
	return m.unmarshalFunc(p, v)
}

// fakeEnvRegistry implements envapi.Registry with just enough to exercise
// resolveAuthKey. Other methods panic so unexpected calls fail loudly.
type fakeEnvRegistry struct {
	getFn func(ctx context.Context, name string) (string, error)
}

func (f *fakeEnvRegistry) Get(ctx context.Context, name string) (string, error) {
	return f.getFn(ctx, name)
}
func (*fakeEnvRegistry) Lookup(context.Context, string) (string, bool, error) {
	panic("not used")
}
func (*fakeEnvRegistry) Set(context.Context, string, string) error { panic("not used") }
func (*fakeEnvRegistry) All(context.Context) (map[string]string, error) {
	panic("not used")
}
func (*fakeEnvRegistry) GetStorage(context.Context, registry.ID) (envapi.Storage, error) {
	panic("not used")
}
func (*fakeEnvRegistry) RegisterStorage(registry.ID, envapi.Storage) { panic("not used") }

func makeTailscaleEntry() registry.Entry {
	return registry.Entry{
		ID:   registry.NewID("app.net", "node"),
		Kind: netapi.KindTailscale,
		Data: payload.New(map[string]any{}),
	}
}

// --- Kind ---

func TestDriver_Kind(t *testing.T) {
	assert.Equal(t, netapi.KindTailscale, NewDriver().Kind())
}

// --- resolveAuthKey ---

func TestResolveAuthKey_AlreadySet_Noop(t *testing.T) {
	cfg := &netapi.TailscaleConfig{AuthKey: "tskey-existing", AuthKeyEnv: "TS_KEY"}
	env := &fakeEnvRegistry{getFn: func(context.Context, string) (string, error) {
		t.Fatal("env registry must not be consulted when AuthKey is already set")
		return "", nil
	}}
	err := resolveAuthKey(context.Background(), cfg, netservice.Deps{Env: env})
	require.NoError(t, err)
	assert.Equal(t, "tskey-existing", cfg.AuthKey)
}

func TestResolveAuthKey_NoEnvVarConfigured_Noop(t *testing.T) {
	cfg := &netapi.TailscaleConfig{AuthKey: "tskey-existing"}
	err := resolveAuthKey(context.Background(), cfg, netservice.Deps{})
	require.NoError(t, err)
	assert.Equal(t, "tskey-existing", cfg.AuthKey)
}

func TestResolveAuthKey_EnvRegistryMissing(t *testing.T) {
	cfg := &netapi.TailscaleConfig{AuthKeyEnv: "TS_KEY"}
	err := resolveAuthKey(context.Background(), cfg, netservice.Deps{Env: nil})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TS_KEY")
	assert.Empty(t, cfg.AuthKey)
}

func TestResolveAuthKey_LookupFails(t *testing.T) {
	lookupErr := errors.New("secret backend down")
	cfg := &netapi.TailscaleConfig{AuthKeyEnv: "TS_KEY"}
	env := &fakeEnvRegistry{getFn: func(_ context.Context, name string) (string, error) {
		assert.Equal(t, "TS_KEY", name)
		return "", lookupErr
	}}
	err := resolveAuthKey(context.Background(), cfg, netservice.Deps{Env: env})
	require.Error(t, err)
	assert.ErrorIs(t, err, lookupErr)
	assert.Empty(t, cfg.AuthKey)
}

func TestResolveAuthKey_Resolves(t *testing.T) {
	cfg := &netapi.TailscaleConfig{AuthKeyEnv: "TS_KEY"}
	env := &fakeEnvRegistry{getFn: func(_ context.Context, name string) (string, error) {
		assert.Equal(t, "TS_KEY", name)
		return "tskey-resolved", nil
	}}
	err := resolveAuthKey(context.Background(), cfg, netservice.Deps{Env: env})
	require.NoError(t, err)
	assert.Equal(t, "tskey-resolved", cfg.AuthKey)
}

// --- resolveStateDir ---

func TestResolveStateDir_AlreadySet_Noop(t *testing.T) {
	cfg := &netapi.TailscaleConfig{StateDir: "/custom/path"}
	resolveStateDir(cfg, registry.NewID("app.net", "node"), netservice.Deps{StateDir: "/var/state"})
	assert.Equal(t, "/custom/path", cfg.StateDir)
}

func TestResolveStateDir_HostnameTakesPrecedence(t *testing.T) {
	cfg := &netapi.TailscaleConfig{Hostname: "worker-1"}
	resolveStateDir(cfg, registry.NewID("app.net", "entry-name"), netservice.Deps{StateDir: "/var/state"})
	assert.Equal(t, filepath.Join("/var/state", "tailscale", "worker-1"), cfg.StateDir)
}

func TestResolveStateDir_FallsBackToEntryName(t *testing.T) {
	cfg := &netapi.TailscaleConfig{}
	resolveStateDir(cfg, registry.NewID("app.net", "entry-name"), netservice.Deps{StateDir: "/var/state"})
	assert.Equal(t, filepath.Join("/var/state", "tailscale", "entry-name"), cfg.StateDir)
}

func TestResolveStateDir_NoBase_LeavesEmpty(t *testing.T) {
	cfg := &netapi.TailscaleConfig{Hostname: "worker-1"}
	resolveStateDir(cfg, registry.NewID("app.net", "entry-name"), netservice.Deps{})
	assert.Empty(t, cfg.StateDir)
}

// --- Create (error paths only — NewService starts tsnet which we won't do in unit tests) ---

func TestDriver_Create_DecodeError(t *testing.T) {
	decodeErr := errors.New("bad config bytes")
	dtt := &mockTranscoder{
		unmarshalFunc: func(payload.Payload, any) error { return decodeErr },
	}
	svc, err := NewDriver().Create(context.Background(), makeTailscaleEntry(), netservice.Deps{Transcoder: dtt})
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "tailscale")
	assert.ErrorIs(t, err, decodeErr)
}

func TestDriver_Create_ValidationError_MissingAuth(t *testing.T) {
	dtt := &mockTranscoder{
		unmarshalFunc: func(payload.Payload, any) error {
			// leave both AuthKey and AuthKeyEnv empty → Validate rejects
			return nil
		},
	}
	svc, err := NewDriver().Create(context.Background(), makeTailscaleEntry(), netservice.Deps{Transcoder: dtt})
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.ErrorIs(t, err, netapi.ErrAuthKeyRequired)
}

func TestDriver_Create_AuthKeyEnvWithoutRegistry(t *testing.T) {
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v any) error {
			cfg := v.(*netapi.TailscaleConfig)
			cfg.AuthKeyEnv = "TS_KEY"
			return nil
		},
	}
	// Env is nil → resolveAuthKey returns EnvRegistryUnavailable.
	svc, err := NewDriver().Create(context.Background(), makeTailscaleEntry(), netservice.Deps{Transcoder: dtt, Env: nil})
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "TS_KEY")
}

func TestDriver_Create_AuthKeyEnvLookupFails(t *testing.T) {
	lookupErr := errors.New("secret backend down")
	dtt := &mockTranscoder{
		unmarshalFunc: func(_ payload.Payload, v any) error {
			cfg := v.(*netapi.TailscaleConfig)
			cfg.AuthKeyEnv = "TS_KEY"
			return nil
		},
	}
	env := &fakeEnvRegistry{getFn: func(context.Context, string) (string, error) { return "", lookupErr }}
	svc, err := NewDriver().Create(context.Background(), makeTailscaleEntry(), netservice.Deps{Transcoder: dtt, Env: env})
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.ErrorIs(t, err, lookupErr)
}
