// SPDX-License-Identifier: MPL-2.0

package core

import (
	"testing"

	contextapi "github.com/wippyai/runtime/api/context"
	securityapi "github.com/wippyai/runtime/api/security"
)

func TestSecurityDefaultsToStrictMode(t *testing.T) {
	ctx, err := Security().Load(contextapi.NewRootContext())
	if err != nil {
		t.Fatal(err)
	}
	if !securityapi.IsStrictMode(ctx) {
		t.Fatal("security must default to strict mode")
	}
}
