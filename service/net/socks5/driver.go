// SPDX-License-Identifier: MPL-2.0

package socks5

import (
	"context"

	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	entryutil "github.com/wippyai/runtime/internal/entry"
	netservice "github.com/wippyai/runtime/service/net"
)

// Driver implements netservice.Driver for the SOCKS5 overlay kind.
type Driver struct{}

// NewDriver returns a ready-to-register SOCKS5 Driver.
func NewDriver() Driver { return Driver{} }

// Kind returns netapi.KindSOCKS5.
func (Driver) Kind() registry.Kind { return netapi.KindSOCKS5 }

// Create decodes the entry into a SOCKS5Config and returns a bound Service.
// Stateless: SOCKS5 keeps nothing on disk.
func (Driver) Create(ctx context.Context, entry registry.Entry, deps netservice.Deps) (netapi.Service, error) {
	cfg, err := entryutil.DecodeEntryConfig[netapi.SOCKS5Config](ctx, deps.Transcoder, entry)
	if err != nil {
		return nil, netservice.NewDecodeConfigError("socks5", err)
	}
	return NewService(cfg)
}
