package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	netapi "github.com/wippyai/runtime/api/net"
	netservice "github.com/wippyai/runtime/service/net"
)

func Network() boot.Component {
	return boot.New(boot.P{
		Name: NetworkName,
		Load: func(ctx context.Context) (context.Context, error) {
			svc := netservice.NewSecureService()
			return netapi.WithService(ctx, svc), nil
		},
	})
}
