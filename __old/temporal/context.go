package temporal

import (
	"github.com/ponyruntime/pony/api/context"
)

var (
	// ClientCtx stores the current client instance for activity context and others.
	ClientCtx = &context.Key{Name: "temporal.client"} //nolint:gochecknoglobals
)
