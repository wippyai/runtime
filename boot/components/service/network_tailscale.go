// SPDX-License-Identifier: MPL-2.0

//go:build tailscale

package service

import (
	netservice "github.com/wippyai/runtime/service/net"
	"github.com/wippyai/runtime/service/net/tailscale"
)

func newTailscaleDriver() netservice.Driver {
	return tailscale.NewDriver()
}
