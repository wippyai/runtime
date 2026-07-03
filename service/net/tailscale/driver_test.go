// SPDX-License-Identifier: MPL-2.0

package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	netservice "github.com/wippyai/runtime/service/net"
	entryutil "github.com/wippyai/runtime/system/entry"
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

// jsonMapTranscoder unmarshals a Golang map payload into a struct via a JSON
// round trip so the central auth_key_env resolution path decodes against the
// config's json tags end to end.
type jsonMapTranscoder struct{}

func (jsonMapTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return payload.New(p.Data()), nil
}

func (jsonMapTranscoder) Unmarshal(p payload.Payload, v any) error {
	m, ok := p.Data().(map[string]any)
	if !ok {
		return fmt.Errorf("payload is not a map")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// appContext mirrors production, where the boot pipeline seeds an app context
// on the dispatch ctx; env.WithRegistry stores into it.
func appContext() context.Context {
	return ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
}

// authKeyEnvEntry builds a Tailscale entry whose raw data carries an
// auth_key_env directive naming the given variable.
func authKeyEnvEntry(variable string) registry.Entry {
	return registry.Entry{
		ID:   registry.NewID("app.net", "node"),
		Kind: netapi.KindTailscale,
		Data: payload.New(map[string]any{"auth_key_env": variable}),
	}
}

// fakeEnvRegistry implements envapi.Registry with just enough to exercise
// auth_key_env resolution. Other methods panic so unexpected calls fail loudly.
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
func (*fakeEnvRegistry) RegisterVariable(envapi.Variable) error      { return nil }
func (*fakeEnvRegistry) UnregisterVariable(registry.ID)              {}

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

// --- auth_key_env resolution via the central decode pass ---

// A decode with the env registry injected resolves an auth_key_env directive
// into cfg.AuthKey. This is the seam Driver.Create relies on; asserting it
// directly avoids starting a real tsnet node on the success path.
func TestAuthKeyEnv_ResolvesWhenRegistryInjected(t *testing.T) {
	env := &fakeEnvRegistry{getFn: func(_ context.Context, name string) (string, error) {
		assert.Equal(t, "TS_KEY", name)
		return "tskey-resolved", nil
	}}
	ctx := envapi.WithRegistry(appContext(), env)

	cfg, err := entryutil.DecodeEntryConfig[netapi.TailscaleConfig](ctx, jsonMapTranscoder{}, authKeyEnvEntry("TS_KEY"))
	require.NoError(t, err)
	assert.Equal(t, "tskey-resolved", cfg.AuthKey)
}

// Without an env registry in context the directive is skipped, leaving AuthKey
// empty; Validate then reports the key as not configured.
func TestAuthKeyEnv_SkippedWithoutRegistry(t *testing.T) {
	cfg, err := entryutil.DecodeEntryConfig[netapi.TailscaleConfig](context.Background(), jsonMapTranscoder{}, authKeyEnvEntry("TS_KEY"))
	require.Error(t, err)
	assert.ErrorIs(t, err, netapi.ErrAuthKeyRequired)
	assert.Nil(t, cfg)
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
			// leave AuthKey empty → Validate rejects
			return nil
		},
	}
	svc, err := NewDriver().Create(context.Background(), makeTailscaleEntry(), netservice.Deps{Transcoder: dtt})
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.ErrorIs(t, err, netapi.ErrAuthKeyRequired)
}

// With deps.Env nil the driver attaches no registry, the auth_key_env
// directive is skipped, and Validate rejects the empty AuthKey. This replaces
// the former hard EnvRegistryUnavailable failure: an unresolved env-backed key
// now surfaces as the same "not configured" error as a missing key.
func TestDriver_Create_AuthKeyEnvWithoutRegistry(t *testing.T) {
	svc, err := NewDriver().Create(appContext(), authKeyEnvEntry("TS_KEY"), netservice.Deps{Transcoder: jsonMapTranscoder{}, Env: nil})
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.ErrorIs(t, err, netapi.ErrAuthKeyRequired)
}

// A lookup failure surfaces through Create only because the driver attaches
// deps.Env to the decode context; without the attach the directive would be
// silently skipped.
func TestDriver_Create_AuthKeyEnvLookupFails(t *testing.T) {
	lookupErr := errors.New("secret backend down")
	env := &fakeEnvRegistry{getFn: func(context.Context, string) (string, error) { return "", lookupErr }}
	svc, err := NewDriver().Create(appContext(), authKeyEnvEntry("TS_KEY"), netservice.Deps{Transcoder: jsonMapTranscoder{}, Env: env})
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.ErrorIs(t, err, lookupErr)
}
