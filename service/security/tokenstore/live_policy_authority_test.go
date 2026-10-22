// SPDX-License-Identifier: MPL-2.0

package tokenstore_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
)

func TestLiveAuthorityPolicyIDs(t *testing.T) {
	for _, tc := range []struct {
		response       attrs.Bag
		name           string
		records, admin security.Result
		wantError      bool
	}{
		{name: "selected policy", response: attrs.Bag{"scope_policy_ids": []string{"app:read"}}, records: security.Allow},
		{name: "selection revoked", response: attrs.Bag{"scope_policy_ids": []string{}}},
		{name: "field absent"},
		{name: "group union", response: attrs.Bag{"groups": []string{"app:current"}, "scope_policy_ids": []string{"app:read"}}, records: security.Allow, admin: security.Allow},
		{name: "ceiling", response: attrs.Bag{"scope_policy_ids": []string{"app:read", "app:write"}, "ceiling": attrs.Bag{"policies": []string{"app:read"}}}, records: security.Allow},
		{name: "empty ceiling", response: attrs.Bag{"scope_policy_ids": []string{"app:read"}, "ceiling": attrs.Bag{}}},
		{name: "unavailable policy", response: attrs.Bag{"scope_policy_ids": []string{"app:read", "app:missing"}}, wantError: true},
		{name: "malformed selection", response: attrs.Bag{"scope_policy_ids": true}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, store, token, _, _ := authorityBoundaryFixture(t, &jsonTranscoder{})
			response := attrs.Bag{"subject_id": "member", "groups": []string{}}
			for key, value := range tc.response {
				response[key] = value
			}
			ctx = function.WithRegistry(ctx, lookupFunc(func(context.Context, runtimeapi.Task) (*runtimeapi.Result, error) {
				return &runtimeapi.Result{Value: payload.New(response)}, nil
			}))
			actor, scope, err := store.Validate(ctx, token)
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, scope)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.records, scope.Evaluate(actor, "read", "records", nil))
			require.Equal(t, tc.admin, scope.Evaluate(actor, "read", "admin", nil))
		})
	}
}

func BenchmarkValidateLivePolicyIDs(b *testing.B) {
	for _, count := range []int{0, 5, 50} {
		b.Run(fmt.Sprintf("policies_%d", count), func(b *testing.B) {
			ctx, store, token, reg, _ := authorityBoundaryFixture(b, &jsonTranscoder{})
			ids := make([]registry.ID, count)
			for i := range ids {
				ids[i] = registry.NewID("app", fmt.Sprintf("policy_%d", i))
				reg.AddPolicy(ids[i], security.Allow)
			}
			response := attrs.Bag{"subject_id": "member", "groups": []string{"app:current"}}
			if count > 0 {
				response["scope_policy_ids"] = ids
			}
			ctx = function.WithRegistry(ctx, lookupFunc(func(context.Context, runtimeapi.Task) (*runtimeapi.Result, error) {
				return &runtimeapi.Result{Value: payload.New(response)}, nil
			}))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, _, err := store.Validate(ctx, token)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
