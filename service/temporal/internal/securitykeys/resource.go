// SPDX-License-Identifier: MPL-2.0

package securitykeys

import (
	"context"

	api "github.com/wippyai/runtime/api/service/temporal"
)

type accessKey struct{}
type keysKey struct{}

type Resource struct {
	client api.ClientResource
	keys   [][]byte
}

func WithAccess(ctx context.Context) context.Context {
	return context.WithValue(ctx, accessKey{}, true)
}

func Requested(ctx context.Context) bool {
	requested, _ := ctx.Value(accessKey{}).(bool)
	return requested
}

func WithKeys(ctx context.Context, keys ...[]byte) context.Context {
	return context.WithValue(ctx, keysKey{}, cloneKeys(keys))
}

func Keys(ctx context.Context) [][]byte {
	keys, _ := ctx.Value(keysKey{}).([][]byte)
	return cloneKeys(keys)
}

func NewResource(client api.ClientResource, keys [][]byte) Resource {
	return Resource{client: client, keys: cloneKeys(keys)}
}

func (r Resource) Client() api.ClientResource {
	return r.client
}

func (r Resource) Keys() [][]byte {
	return cloneKeys(r.keys)
}

func cloneKeys(keys [][]byte) [][]byte {
	cloned := make([][]byte, len(keys))
	for i, key := range keys {
		cloned[i] = append([]byte(nil), key...)
	}
	return cloned
}
