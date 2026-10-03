// SPDX-License-Identifier: MPL-2.0

//go:build tailscale

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
)

func TestTailscaleDriverEnabled(t *testing.T) {
	driver := newTailscaleDriver()
	require.NotNil(t, driver)
	require.Equal(t, netapi.KindTailscale, driver.Kind())
}
