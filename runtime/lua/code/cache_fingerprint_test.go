// SPDX-License-Identifier: MPL-2.0

package code

import (
	"testing"

	"github.com/stretchr/testify/assert"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
)

func TestCacheCompilerVersion(t *testing.T) {
	v := CacheCompilerVersion()
	assert.Equal(t, "lua-cache-v4", v)
}

func TestEffectiveMethod(t *testing.T) {
	assert.Empty(t, EffectiveMethod(luaapi.Library, "ignored"))
	assert.Empty(t, EffectiveMethod(luaapi.LibraryBytecode, "ignored"))
	assert.Equal(t, "handle", EffectiveMethod(luaapi.Function, "handle"))
}

func TestTypecheckConfigHash_Deterministic(t *testing.T) {
	cfg := TypeCheckConfig{
		Enabled: true,
		Strict:  true,
		Rules: TypeCheckRules{
			TypeCheck: true,
			NilCheck:  true,
		},
	}

	h1 := TypecheckConfigHash(cfg)
	h2 := TypecheckConfigHash(cfg)
	assert.Equal(t, h1, h2)
	assert.Len(t, h1, 64) // sha256 hex
}

func TestTypecheckConfigHash_DifferentConfigs(t *testing.T) {
	cfg1 := TypeCheckConfig{Enabled: true}
	cfg2 := TypeCheckConfig{Enabled: false}

	assert.NotEqual(t, TypecheckConfigHash(cfg1), TypecheckConfigHash(cfg2))
}

func TestManifestHash_Nil(t *testing.T) {
	assert.Equal(t, "", ManifestHash(nil))
}

func TestBuiltinManifestHash_Empty(t *testing.T) {
	assert.Equal(t, "", BuiltinManifestHash(nil))
}

const testToolchainIdentity = "test-toolchain-identity"

func TestCompileFingerprint_Deterministic(t *testing.T) {
	deps := []cache.DepFingerprint{
		{Alias: "lib", ID: "app.lib", Fingerprint: "abc123"},
	}

	f1 := CompileFingerprint(testToolchainIdentity, "app.main", "function.lua", "hash1", "handler", deps)
	f2 := CompileFingerprint(testToolchainIdentity, "app.main", "function.lua", "hash1", "handler", deps)
	assert.Equal(t, f1, f2)
	assert.Len(t, f1, 64)
}

func TestCompileFingerprint_DifferentInputs(t *testing.T) {
	f1 := CompileFingerprint(testToolchainIdentity, "app.main", "function.lua", "hash1", "handler", nil)
	f2 := CompileFingerprint(testToolchainIdentity, "app.main", "function.lua", "hash2", "handler", nil)
	assert.NotEqual(t, f1, f2)
}

func TestTypecheckFingerprint_Deterministic(t *testing.T) {
	deps := []cache.DepFingerprint{
		{Alias: "lib", ID: "app.lib", Fingerprint: "abc"},
	}

	f1 := TypecheckFingerprint(testToolchainIdentity, "app.main", "function.lua", "src1", "handler", "tc1", "builtin1", deps)
	f2 := TypecheckFingerprint(testToolchainIdentity, "app.main", "function.lua", "src1", "handler", "tc1", "builtin1", deps)
	assert.Equal(t, f1, f2)
}

func TestTypecheckFingerprint_DifferentTypecheckHash(t *testing.T) {
	f1 := TypecheckFingerprint(testToolchainIdentity, "app.main", "function.lua", "src1", "handler", "tc1", "builtin1", nil)
	f2 := TypecheckFingerprint(testToolchainIdentity, "app.main", "function.lua", "src1", "handler", "tc2", "builtin1", nil)
	assert.NotEqual(t, f1, f2)
}

func TestCompileFingerprint_NoDeps(t *testing.T) {
	f := CompileFingerprint(testToolchainIdentity, "app.main", "function.lua", "hash1", "handler", nil)
	assert.Len(t, f, 64)
}

func TestCompileFingerprint_DepOrderIndependent(t *testing.T) {
	deps1 := []cache.DepFingerprint{
		{Alias: "a", ID: "app.a", Fingerprint: "fa"},
		{Alias: "b", ID: "app.b", Fingerprint: "fb"},
	}
	deps2 := []cache.DepFingerprint{
		{Alias: "b", ID: "app.b", Fingerprint: "fb"},
		{Alias: "a", ID: "app.a", Fingerprint: "fa"},
	}

	f1 := CompileFingerprint(testToolchainIdentity, "app.main", "function.lua", "hash1", "handler", deps1)
	f2 := CompileFingerprint(testToolchainIdentity, "app.main", "function.lua", "hash1", "handler", deps2)
	assert.Equal(t, f1, f2)
}

func TestFingerprints_ToolchainIdentity(t *testing.T) {
	// Identity A
	idA := "toolchain-identity-A"
	compileA1 := CompileFingerprint(idA, "app.main", "function.lua", "hash1", "handler", nil)
	compileA2 := CompileFingerprint(idA, "app.main", "function.lua", "hash1", "handler", nil)
	assert.Equal(t, compileA1, compileA2)

	typecheckA1 := TypecheckFingerprint(idA, "app.main", "function.lua", "hash1", "handler", "tc1", "bi1", nil)
	typecheckA2 := TypecheckFingerprint(idA, "app.main", "function.lua", "hash1", "handler", "tc1", "bi1", nil)
	assert.Equal(t, typecheckA1, typecheckA2)

	runtimeA1 := RuntimeFingerprint(idA, "app.main", "function.lua", "hash1", "handler", 1, nil)
	runtimeA2 := RuntimeFingerprint(idA, "app.main", "function.lua", "hash1", "handler", 1, nil)
	assert.Equal(t, runtimeA1, runtimeA2)

	// Identity B
	idB := "toolchain-identity-B"
	compileB := CompileFingerprint(idB, "app.main", "function.lua", "hash1", "handler", nil)
	assert.NotEqual(t, compileA1, compileB)

	typecheckB := TypecheckFingerprint(idB, "app.main", "function.lua", "hash1", "handler", "tc1", "bi1", nil)
	assert.NotEqual(t, typecheckA1, typecheckB)

	runtimeB := RuntimeFingerprint(idB, "app.main", "function.lua", "hash1", "handler", 1, nil)
	assert.NotEqual(t, runtimeA1, runtimeB)
}
