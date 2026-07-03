// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	entrycfg "github.com/wippyai/runtime/system/entry"
	systempayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
)

// collisionTestContext returns a context carrying a working JSON transcoder and
// an env registry that resolves nothing.
func collisionTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)
	return envapi.WithRegistry(ctx, emptyEnvRegistry{})
}

// emptyEnvRegistry is an env.Registry that resolves no variables. A lookup of
// any name reports "not found" without error.
type emptyEnvRegistry struct{}

func (emptyEnvRegistry) Get(context.Context, string) (string, error) {
	return "", envapi.ErrVariableNotFound
}
func (emptyEnvRegistry) Lookup(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (emptyEnvRegistry) Set(context.Context, string, string) error      { return nil }
func (emptyEnvRegistry) All(context.Context) (map[string]string, error) { return nil, nil }
func (emptyEnvRegistry) GetStorage(context.Context, registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrStorageNotFound
}
func (emptyEnvRegistry) RegisterStorage(registry.ID, envapi.Storage) {}
func (emptyEnvRegistry) RegisterVariable(envapi.Variable) error      { return nil }
func (emptyEnvRegistry) UnregisterVariable(registry.ID)              {}

// TestDecodeFunctionConfig_SourceIsNeverResolved pins the resolve:"-" contract:
// source is code, so every placeholder-shaped span in it — bare shell tokens and
// explicit ${env:...} references alike — survives decode byte-identical.
func TestDecodeFunctionConfig_SourceIsNeverResolved(t *testing.T) {
	ctx := collisionTestContext()

	data := `{"source":"local home = \"${UNSET_VAR}/${env:OTHER_VAR}\"\nreturn home","method":"handler","pool":{"type":"static","size":1}}`
	entry := registry.Entry{
		ID:   registry.NewID("app.test", "fn"),
		Kind: api.Function,
		Data: payload.NewPayload(data, payload.JSON),
	}

	cfg, err := entrycfg.DecodeEntryConfigFromContext[api.FunctionConfig](ctx, entry)
	require.NoError(t, err)
	assert.Contains(t, cfg.Source, `${UNSET_VAR}/${env:OTHER_VAR}`)
}

// TestDecodeFunctionConfig_UnsetEnvReferenceOutsideSource is the strict side of
// the boundary: an unresolved ${env:...} reference in a config field that is not
// excluded from resolution hard-fails the decode.
func TestDecodeFunctionConfig_UnsetEnvReferenceOutsideSource(t *testing.T) {
	ctx := collisionTestContext()

	data := `{"source":"return 1","method":"${env:UNSET_VAR}","pool":{"type":"static","size":1}}`
	entry := registry.Entry{
		ID:   registry.NewID("app.test", "fn"),
		Kind: api.Function,
		Data: payload.NewPayload(data, payload.JSON),
	}

	_, err := entrycfg.DecodeEntryConfigFromContext[api.FunctionConfig](ctx, entry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UNSET_VAR")
}

// TestDecodeFunctionConfig_SourceWithoutPlaceholder is the control: identical
// source without a placeholder token decodes cleanly.
func TestDecodeFunctionConfig_SourceWithoutPlaceholder(t *testing.T) {
	ctx := collisionTestContext()

	data := `{"source":"local home = \"/data\"\nreturn home","method":"handler","pool":{"type":"static","size":1}}`
	entry := registry.Entry{
		ID:   registry.NewID("app.test", "fn"),
		Kind: api.Function,
		Data: payload.NewPayload(data, payload.JSON),
	}

	cfg, err := entrycfg.DecodeEntryConfigFromContext[api.FunctionConfig](ctx, entry)
	require.NoError(t, err)
	assert.Equal(t, "handler", cfg.Method)
}

// TestDecodeFunctionConfig_InjectsEntryMeta pins the Meta-injection delta: the
// central helper populates cfg.Meta from entry.Meta when the data carries no
// meta field, so options authored on the entry become visible to the runtime.
func TestDecodeFunctionConfig_InjectsEntryMeta(t *testing.T) {
	ctx := collisionTestContext()

	data := `{"source":"return 1","method":"handler","pool":{"type":"static","size":1}}`
	entry := registry.Entry{
		ID:   registry.NewID("app.test", "fn"),
		Kind: api.Function,
		Data: payload.NewPayload(data, payload.JSON),
		Meta: attrs.Bag{"options": map[string]any{"public": true}},
	}

	cfg, err := entrycfg.DecodeEntryConfigFromContext[api.FunctionConfig](ctx, entry)
	require.NoError(t, err)

	opts, ok := cfg.Meta.GetBag("options")
	require.True(t, ok)
	assert.True(t, opts.GetBool("public", false))
}

// TestDecodeFunctionConfig_NoLeakBackToRegistry proves resolution never writes
// resolved values back into the sealed registry entry and never touches source:
// the decoded struct carries resolved fields while entry.Data keeps the original
// placeholders, so a resolved secret cannot reach Lua through the registry.
func TestDecodeFunctionConfig_NoLeakBackToRegistry(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	json.Register(transcoder)
	ctx = payload.WithTranscoder(ctx, transcoder)
	ctx = envapi.WithRegistry(ctx, resolvingEnvRegistry{value: "resolved-method"})

	dataJSON := `{"source":"local x = ${SECRET}/${env:OTHER}","method":"${env:METHOD_VAR}","pool":{"type":"static","size":1}}`
	entry := registry.Entry{
		ID:   registry.NewID("app.test", "fn"),
		Kind: api.Function,
		Data: payload.NewPayload(dataJSON, payload.JSON),
	}

	cfg, err := entrycfg.DecodeEntryConfigFromContext[api.FunctionConfig](ctx, entry)
	require.NoError(t, err)

	// The non-source field resolved in the returned struct.
	assert.Equal(t, "resolved-method", cfg.Method)
	// Source is exempt: every placeholder-shaped span survives verbatim.
	assert.Contains(t, cfg.Source, `${SECRET}/${env:OTHER}`)
	// The sealed entry payload still holds the original method placeholder.
	raw, ok := entry.Data.Data().(string)
	require.True(t, ok)
	assert.Contains(t, raw, `${env:METHOD_VAR}`, "registry entry must keep the placeholder, not the resolved value")
	assert.NotContains(t, raw, "resolved-method", "resolved value must not leak back into the registry entry")
}

// resolvingEnvRegistry resolves every variable to a fixed value.
type resolvingEnvRegistry struct {
	value string
}

func (r resolvingEnvRegistry) Get(context.Context, string) (string, error) { return r.value, nil }
func (r resolvingEnvRegistry) Lookup(context.Context, string) (string, bool, error) {
	return r.value, true, nil
}
func (resolvingEnvRegistry) Set(context.Context, string, string) error      { return nil }
func (resolvingEnvRegistry) All(context.Context) (map[string]string, error) { return nil, nil }
func (resolvingEnvRegistry) GetStorage(context.Context, registry.ID) (envapi.Storage, error) {
	return nil, envapi.ErrStorageNotFound
}
func (resolvingEnvRegistry) RegisterStorage(registry.ID, envapi.Storage) {}
func (resolvingEnvRegistry) RegisterVariable(envapi.Variable) error      { return nil }
func (resolvingEnvRegistry) UnregisterVariable(registry.ID)              {}
