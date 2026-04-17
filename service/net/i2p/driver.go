// SPDX-License-Identifier: MPL-2.0

package i2p

import (
	"context"

	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	entryutil "github.com/wippyai/runtime/internal/entry"
	netservice "github.com/wippyai/runtime/service/net"
)

// Driver implements netservice.Driver for the I2P overlay kind.
type Driver struct{}

// NewDriver returns a ready-to-register I2P Driver.
func NewDriver() Driver { return Driver{} }

// Kind returns netapi.KindI2P.
func (Driver) Kind() registry.Kind { return netapi.KindI2P }

// Create decodes the entry into an I2PConfig and returns a bound Service.
// The SAM session is opened per-dial, so Create itself does no I/O and
// never needs state directories.
func (Driver) Create(ctx context.Context, entry registry.Entry, deps netservice.Deps) (netapi.Service, error) {
	cfg, err := entryutil.DecodeEntryConfig[netapi.I2PConfig](ctx, deps.Transcoder, entry)
	if err != nil {
		return nil, netservice.NewDecodeConfigError("i2p", err)
	}
	return NewService(cfg)
}
