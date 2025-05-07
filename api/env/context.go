// Package env provides access to environment variables with flexible storage backends.
package env

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context key for the environment registry
var registryCtx = &ctxapi.Key{Name: "env.registry"}

// var storeCtx = &ctxapi.Key{Name: "env.store"}

// WithRegistry returns a new context with the provided Registry attached
func WithRegistry(ctx context.Context, reg Registry) context.Context {
	return context.WithValue(ctx, registryCtx, reg)
}

// GetRegistry retrieves the environment registry from the context
func GetRegistry(ctx context.Context) Registry {
	if reg, ok := ctx.Value(registryCtx).(Registry); ok {
		return reg
	}
	return nil
}

// func GetStore(ctx context.Context) string {
// 	if store, ok := ctx.Value(storeCtx).(string); ok {
// 		return store
// 	}
// 	return ""
// }

//// Get retrieves a variable's value using the registry from context
//func Get(ctx context.Context, name string) (string, error) {
//	reg := GetRegistry(ctx)
//	if reg == nil {
//		return "", errors.New("environment registry not found in context")
//	}
//	return reg.Get(ctx, name)
//}
//
//// Set sets a variable's value using the registry from context
//func Set(ctx context.Context, name, value string) error {
//	reg := GetRegistry(ctx)
//	if reg == nil {
//		return errors.New("environment registry not found in context")
//	}
//	return reg.Set(ctx, name, value)
//}
//
//// All returns all environment variables and their values
//func All(ctx context.Context) (map[string]string, error) {
//	reg := GetRegistry(ctx)
//	if reg == nil {
//		return nil, errors.New("environment registry not found in context")
//	}
//	return reg.All(ctx)
//}
//
//// MustGet retrieves a variable's value or returns the default if not found
//func MustGet(ctx context.Context, name, defaultValue string) string {
//	value, err := Get(ctx, name)
//	if err != nil {
//		return defaultValue
//	}
//	return value
//}
