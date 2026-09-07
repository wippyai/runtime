// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"testing"
)

func TestNativeComponentsCannotReplaceBuiltins(t *testing.T) {
	require.Error(t, validateNativeComponents([]boot.Component{StandardComponents()[0]}))
	component := boot.New(boot.P{Name: "example.native"})
	require.NoError(t, validateNativeComponents([]boot.Component{component}))
	require.Error(t, validateNativeComponents([]boot.Component{component, component}))
	require.Error(t, validateNativeComponents([]boot.Component{nil}))
}
