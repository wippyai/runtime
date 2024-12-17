package http

import (
	"context"
	config "github.com/ponyruntime/pony/api/server/http"
)

// GetRouteInfo retrieves route information from the context
func GetRouteInfo(ctx context.Context) (*config.RouteInfo, bool) {
	info, ok := ctx.Value(config.RouteInfoCtx).(*config.RouteInfo)
	return info, ok
}
