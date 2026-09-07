// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	cdcapi "github.com/wippyai/runtime/api/service/cdc"
	cdcsystem "github.com/wippyai/runtime/system/cdc"
)

// CDC creates the driver-neutral source registry. Concrete drivers are
// injected by the service layer after this component has published the
// registry into the application context.
func CDC() boot.Component {
	return boot.New(boot.P{
		Name: CDCRegistryName,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			return cdcapi.WithRegistry(ctx, cdcsystem.NewRegistry(logger.Named("cdc"))), nil
		},
	})
}
