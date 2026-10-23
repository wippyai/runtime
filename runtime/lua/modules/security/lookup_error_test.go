// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
)

type lookupErrorRegistry struct{ err error }

func (r lookupErrorRegistry) GetPolicy(registry.ID) (secapi.Policy, error)     { return nil, r.err }
func (r lookupErrorRegistry) GetPolicyGroup(registry.ID) (secapi.Scope, error) { return nil, r.err }
func (lookupErrorRegistry) ListGroups() []registry.ID                          { return nil }
func (lookupErrorRegistry) ListPolicies() []registry.ID                        { return nil }

func TestPolicyLookupErrorMetadata(t *testing.T) {
	lua.SetErrorMetadataExtractor(func(err error) *lua.ErrorMetadata {
		root := apierror.BuildChain(err).Root()
		return &lua.ErrorMetadata{Kind: lua.Kind(root.Kind), Retryable: root.Retryable, Details: root.Details}
	})
	t.Cleanup(func() { lua.SetErrorMetadataExtractor(nil) })
	for _, name := range []string{"policy", "named_scope"} {
		for _, kind := range []apierror.Kind{apierror.NotFound, apierror.PermissionDenied, apierror.Unavailable} {
			for _, retryable := range []apierror.Ternary{apierror.False, apierror.True, apierror.Unspecified} {
				t.Run(fmt.Sprintf("%s/%s/%s", name, kind, retryable), func(t *testing.T) {
					cause := apierror.New(kind, "lookup fails").WithRetryable(retryable).
						WithDetails(attrs.Bag{"code": "policy_lookup"})
					l := setupStateWithSecurityContext(secapi.Actor{ID: "member"},
						secsystem.NewScope([]secapi.Policy{newMockPolicy("app", "all", secapi.Allow)}))
					defer l.Close()
					l.SetContext(secapi.WithRegistry(l.Context(), lookupErrorRegistry{err: cause}))
					if err := l.DoString("result, lookup_error = security." + name + "('app:missing')"); err != nil {
						t.Fatal(err)
					}
					if l.GetGlobal("result") != lua.LNil {
						t.Fatal("lookup failure returns a value")
					}
					got, ok := l.GetGlobal("lookup_error").(*lua.Error)
					if !ok {
						t.Fatal("lookup does not return a structured error")
					}
					if got.Kind() != lua.Kind(kind) {
						t.Errorf("kind = %s, want %s", got.Kind(), kind)
					}
					wantRetry := map[apierror.Ternary]lua.Ternary{apierror.False: lua.TernaryFalse, apierror.True: lua.TernaryTrue, apierror.Unspecified: lua.TernaryUnknown}[retryable]
					if got.Retryable() != wantRetry {
						t.Errorf("retryable = %s, want %s", got.Retryable(), wantRetry)
					}
					if got.Details()["code"] != "policy_lookup" {
						t.Errorf("details = %v", got.Details())
					}
				})
			}
		}
	}
}
