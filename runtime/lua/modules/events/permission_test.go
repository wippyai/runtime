// SPDX-License-Identifier: MPL-2.0

package events

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	regapi "github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
)

// denyAllPolicy refuses every action.
type denyAllPolicy struct{}

func (denyAllPolicy) ID() regapi.ID { return regapi.NewID("test", "deny-all") }

func (denyAllPolicy) Evaluate(_ secapi.Actor, _, _ string, _ attrs.Bag) secapi.Result {
	return secapi.Deny
}

func TestEventsPermissionDenied(t *testing.T) {
	cases := []struct {
		name string
		call string
	}{
		{"subscribe", `events.subscribe("app")`},
		{"send", `events.send("app", "test.kind", "some/path", {})`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := lua.NewState()
			t.Cleanup(func() { l.Close() })
			lua.OpenErrors(l)

			ctx := secapi.SetStrictMode(ctxapi.NewRootContext(), false)
			ctx, fc := ctxapi.OpenFrameContext(ctx)
			t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
			require.NoError(t, secapi.SetActor(ctx, secapi.Actor{ID: "tester"}))
			require.NoError(t, secapi.SetScope(ctx, secsystem.NewScope([]secapi.Policy{denyAllPolicy{}})))
			l.SetContext(ctx)

			tbl, _ := Module.Build()
			l.SetGlobal(Module.Name, tbl)

			require.NoError(t, l.DoString(`
				local v, err = `+tc.call+`
				assert(v == nil, "expected nil result under a deny policy")
				assert(err ~= nil, "expected error under a deny policy")
				assert(err:kind() == errors.PERMISSION_DENIED, "expected PERMISSION_DENIED kind, got: " .. tostring(err:kind()))
				assert(err:retryable() == false, "expected not retryable")
			`))
		})
	}
}
