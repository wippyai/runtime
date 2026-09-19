// SPDX-License-Identifier: MPL-2.0

package toolchain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModuleIdentity_NormalDependency(t *testing.T) {
	id1, err := ModuleIdentity("github.com/stretchr/testify")
	require.NoError(t, err)
	assert.NotEmpty(t, id1)

	id2, err := ModuleIdentity("github.com/stretchr/testify")
	require.NoError(t, err)
	assert.Equal(t, id1, id2)

	luaID, err := ModuleIdentity("github.com/wippyai/go-lua")
	require.NoError(t, err)
	assert.NotEmpty(t, luaID)

	// Dependency identities should differ between distinct modules
	assert.NotEqual(t, id1, luaID)
}

func TestModuleIdentity_UnknownModule(t *testing.T) {
	id1, err := ModuleIdentity("unknown/nonexistent/module/path")
	require.NoError(t, err)
	assert.NotEmpty(t, id1)

	id2, err := ModuleIdentity("unknown/nonexistent/module/path")
	require.NoError(t, err)
	assert.Equal(t, id1, id2)

	exeHash, err := ExecutableSHA256()
	require.NoError(t, err)
	assert.Equal(t, exeHash, id1)

	// Normal dependency should not use the fallback executable hash form
	testifyID, err := ModuleIdentity("github.com/stretchr/testify")
	require.NoError(t, err)
	assert.NotEqual(t, exeHash, testifyID)
}

func TestModuleIdentity_ExecutableError(t *testing.T) {
	prevExe := executableSHA256Fn
	executableSHA256Fn = func() (string, error) {
		return "", NewExecutableError(assert.AnError)
	}
	t.Cleanup(func() { executableSHA256Fn = prevExe })

	id, err := ModuleIdentity("unknown/nonexistent/module/path")
	assert.Error(t, err)
	assert.Empty(t, id)
}
