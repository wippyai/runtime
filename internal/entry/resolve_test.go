// SPDX-License-Identifier: MPL-2.0

package entry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/registry"
)

// fakeVar models an environment variable addressable by both its declared name
// and its registry entry ID.
type fakeVar struct {
	name  string
	id    string
	value string
}

// fakeEnvRegistry is a test double of env.Registry that mirrors the real
// registry's name-then-id lookup resolution.
type fakeEnvRegistry struct {
	vars      []fakeVar
	lookupErr error
}

func (f *fakeEnvRegistry) Lookup(_ context.Context, key string) (string, bool, error) {
	if f.lookupErr != nil {
		return "", false, f.lookupErr
	}
	for _, v := range f.vars {
		if key == v.name || key == v.id {
			if v.value == "" {
				return "", false, nil
			}
			return v.value, true, nil
		}
	}
	return "", false, env.ErrVariableNotFound
}

func (f *fakeEnvRegistry) Get(ctx context.Context, name string) (string, error) {
	value, found, err := f.Lookup(ctx, name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", env.ErrVariableNotFound
	}
	return value, nil
}

func (f *fakeEnvRegistry) Set(context.Context, string, string) error { return nil }

func (f *fakeEnvRegistry) All(context.Context) (map[string]string, error) { return nil, nil }

func (f *fakeEnvRegistry) GetStorage(context.Context, registry.ID) (env.Storage, error) {
	return nil, env.ErrStorageNotFound
}

func (f *fakeEnvRegistry) RegisterStorage(registry.ID, env.Storage) {}

func (f *fakeEnvRegistry) RegisterVariable(env.Variable) error { return nil }

func (f *fakeEnvRegistry) UnregisterVariable(registry.ID) {}

var _ env.Registry = (*fakeEnvRegistry)(nil)

// ctxWithEnv attaches the registry to a fresh AppContext-backed context.
func ctxWithEnv(reg env.Registry) context.Context {
	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	return env.WithRegistry(ctx, reg)
}

func TestExtractPlaceholderNames(t *testing.T) {
	assert.Nil(t, ExtractPlaceholderNames("no placeholders here"))
	assert.Equal(t, []string{"FOO"}, ExtractPlaceholderNames("${env:FOO}"))
	assert.Equal(t, []string{"FOO", "BAR"}, ExtractPlaceholderNames("${FOO|1}-${env:BAR|x}"))
	assert.Equal(t, []string{"app:key"}, ExtractPlaceholderNames("prefix ${env:app:key} suffix"))
	// Duplicates collapse, first-occurrence order preserved.
	assert.Equal(t, []string{"A", "B"}, ExtractPlaceholderNames("${A|1}${B|2}${A|1}"))
	// Escapes, malformed spans, and bare shorthand contribute no names.
	assert.Nil(t, ExtractPlaceholderNames("$${env:FOO} ${lower} ${1BAD} ${BARE}"))
}

func TestResolve_FastPathNoOp(t *testing.T) {
	data := map[string]any{"name": "plain", "nested": map[string]any{"n": 1}}

	// No env registry in context: fast path must not touch it.
	out, err := ResolveDataPlaceholders(context.Background(), data)
	require.NoError(t, err)

	// Same map instance is returned when nothing changes.
	require.True(t, sameMapRef(data, out))
}

func TestResolve_WholeValueStringNoDefault(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "TOKEN", value: "secret"}}}
	data := map[string]any{"token": "${env:TOKEN}"}

	out, err := ResolveDataPlaceholders(ctxWithEnv(reg), data)
	require.NoError(t, err)
	assert.Equal(t, "secret", out["token"])
	// Input is never mutated.
	assert.Equal(t, "${env:TOKEN}", data["token"])
}

func TestResolve_WholeValueNoDefaultNotFound(t *testing.T) {
	reg := &fakeEnvRegistry{}
	data := map[string]any{"token": "${env:MISSING}"}

	_, err := ResolveDataPlaceholders(ctxWithEnv(reg), data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MISSING")
	assert.Contains(t, err.Error(), "token")
}

func TestResolve_TypedDefaults(t *testing.T) {
	cases := []struct {
		name  string
		value string // env value; empty means not found
		def   string
		want  any
	}{
		{name: "int default not found", def: "8080", want: 8080},
		{name: "int coerced", value: "9090", def: "8080", want: 9090},
		{name: "float default not found", def: "1.5", want: 1.5},
		{name: "float coerced", value: "2.5", def: "1.5", want: 2.5},
		{name: "bool default not found", def: "true", want: true},
		{name: "bool coerced", value: "false", def: "true", want: false},
		{name: "string default not found", def: "hello", want: "hello"},
		{name: "quoted default stays string", def: `"8080"`, want: "8080"},
		{name: "quoted default coerced stays string", value: "9090", def: `"8080"`, want: "9090"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var reg *fakeEnvRegistry
			if c.value == "" {
				reg = &fakeEnvRegistry{}
			} else {
				reg = &fakeEnvRegistry{vars: []fakeVar{{name: "V", value: c.value}}}
			}
			data := map[string]any{"field": "${env:V|" + c.def + "}"}
			out, err := ResolveDataPlaceholders(ctxWithEnv(reg), data)
			require.NoError(t, err)
			assert.Equal(t, c.want, out["field"])
		})
	}
}

func TestResolve_CoercionFailure(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "PORT", value: "not-a-number"}}}
	data := map[string]any{"port": "${env:PORT|8080}"}

	_, err := ResolveDataPlaceholders(ctxWithEnv(reg), data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "coerce")
	assert.Contains(t, err.Error(), "PORT")
}

func TestResolve_Interpolation(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "HOST", value: "db"}, {name: "PORT", value: "5432"}}}
	data := map[string]any{"dsn": "postgres://${env:HOST}:${env:PORT}/app"}

	out, err := ResolveDataPlaceholders(ctxWithEnv(reg), data)
	require.NoError(t, err)
	assert.Equal(t, "postgres://db:5432/app", out["dsn"])
}

func TestResolve_InterpolationDefaultAndMissing(t *testing.T) {
	reg := &fakeEnvRegistry{}

	out, err := ResolveDataPlaceholders(ctxWithEnv(reg), map[string]any{"url": "http://${env:HOST|localhost}/x"})
	require.NoError(t, err)
	assert.Equal(t, "http://localhost/x", out["url"])

	_, err = ResolveDataPlaceholders(ctxWithEnv(reg), map[string]any{"url": "http://${env:HOST}/x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HOST")
}

func TestResolve_Escape(t *testing.T) {
	reg := &fakeEnvRegistry{}
	data := map[string]any{"literal": "price is $${env:FOO} always"}

	out, err := ResolveDataPlaceholders(ctxWithEnv(reg), data)
	require.NoError(t, err)
	assert.Equal(t, "price is ${env:FOO} always", out["literal"])
}

func TestResolve_MalformedLeftUntouched(t *testing.T) {
	reg := &fakeEnvRegistry{}
	inputs := []string{"${lower}", "${1BAD}", "${ spaced }", "${Mixed}", "a ${no match} b"}
	for _, in := range inputs {
		data := map[string]any{"v": in}
		out, err := ResolveDataPlaceholders(ctxWithEnv(reg), data)
		require.NoError(t, err, in)
		assert.Equal(t, in, out["v"], in)
	}
}

func TestResolve_RegistryAbsentWithPlaceholder(t *testing.T) {
	data := map[string]any{"token": "${env:TOKEN}"}

	_, err := ResolveDataPlaceholders(context.Background(), data)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrEnvRegistryMissing))
}

func TestResolve_RegistryAbsentButOnlyEscape(t *testing.T) {
	// An escape changes the string but needs no registry.
	out, err := ResolveDataPlaceholders(context.Background(), map[string]any{"v": "$${x}"})
	require.NoError(t, err)
	assert.Equal(t, "${x}", out["v"])
}

func TestResolve_NestedMapsAndSlices(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "A", value: "1"}, {name: "B", value: "two"}}}
	data := map[string]any{
		"outer": map[string]any{
			"inner": "${env:A|0}",
			"list":  []any{"x", "${env:B}", "z"},
		},
		"plain": "untouched",
	}

	out, err := ResolveDataPlaceholders(ctxWithEnv(reg), data)
	require.NoError(t, err)

	outer := out["outer"].(map[string]any)
	assert.Equal(t, 1, outer["inner"])
	assert.Equal(t, []any{"x", "two", "z"}, outer["list"])
	assert.Equal(t, "untouched", out["plain"])
}

func TestResolve_InputImmutability(t *testing.T) {
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "A", value: "1"}}}
	inner := map[string]any{"v": "${env:A|0}"}
	list := []any{"${env:A}"}
	data := map[string]any{"inner": inner, "list": list, "plain": "keep"}

	out, err := ResolveDataPlaceholders(ctxWithEnv(reg), data)
	require.NoError(t, err)

	// Original nested containers are untouched.
	assert.Equal(t, "${env:A|0}", inner["v"])
	assert.Equal(t, "${env:A}", list[0])
	// Resolved copy holds new values in fresh containers.
	assert.Equal(t, 1, out["inner"].(map[string]any)["v"])
	assert.Equal(t, "1", out["list"].([]any)[0])
	// Untouched branch is shared, not copied.
	assert.Equal(t, "keep", out["plain"])
}

func TestResolve_ByNameAndByEntryID(t *testing.T) {
	// A single variable whose entry ID differs from its declared name.
	reg := &fakeEnvRegistry{vars: []fakeVar{{name: "DB_PASSWORD", id: "app:db_password", value: "s3cret"}}}

	byName, err := ResolveDataPlaceholders(ctxWithEnv(reg), map[string]any{"p": "${env:DB_PASSWORD}"})
	require.NoError(t, err)
	assert.Equal(t, "s3cret", byName["p"])

	byID, err := ResolveDataPlaceholders(ctxWithEnv(reg), map[string]any{"p": "${env:app:db_password}"})
	require.NoError(t, err)
	assert.Equal(t, "s3cret", byID["p"])
}

func TestResolve_LookupErrorPropagates(t *testing.T) {
	reg := &fakeEnvRegistry{lookupErr: errors.New("storage down")}
	data := map[string]any{"token": "${env:TOKEN}"}

	_, err := ResolveDataPlaceholders(ctxWithEnv(reg), data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage down")
}

// sameMapRef reports whether two maps are the same underlying instance.
func sameMapRef(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	a["__probe__"] = struct{}{}
	_, ok := b["__probe__"]
	delete(a, "__probe__")
	return ok
}
