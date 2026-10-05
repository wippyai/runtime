// SPDX-License-Identifier: MPL-2.0

//go:build !tailscale

package service

import netservice "github.com/wippyai/runtime/service/net"

func newTailscaleDriver() netservice.Driver {
	return nil
}
