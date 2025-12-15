package temporal

import (
	ctxapi "github.com/wippyai/runtime/api/context"
)

// Context keys for storing temporal-related data
var (
	activityContextKey = &ctxapi.Key{Name: "temporal.activity.context"}
)

// ActivityContextKey returns the context key for activity context storage
func ActivityContextKey() *ctxapi.Key {
	return activityContextKey
}
