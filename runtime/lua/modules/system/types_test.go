// SPDX-License-Identifier: MPL-2.0

package system

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func TestVersionTypeManifest(t *testing.T) {
	t.Run("returns string", func(t *testing.T) {
		tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
		_, diagnostics, err := tc.Check(`
local value: string = require("system").version()
`, "system_version.lua", map[string]*io.Manifest{"system": ModuleTypes()})
		require.NoError(t, err)
		require.False(t, code.HasErrors(diagnostics), "unexpected diagnostics: %v", diagnostics)
	})

	t.Run("rejects number assignment", func(t *testing.T) {
		tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
		_, diagnostics, err := tc.Check(`
local value: number = require("system").version()
`, "system_version_invalid.lua", map[string]*io.Manifest{"system": ModuleTypes()})
		require.NoError(t, err)
		require.True(t, code.HasErrors(diagnostics), "expected a type diagnostic")
	})
}
