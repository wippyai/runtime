// SPDX-License-Identifier: MPL-2.0

package error

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
)

type stackBaseError struct{ base Error }

func (e stackBaseError) Error() string             { return e.base.Error() }
func (e stackBaseError) Kind() Kind                { return e.base.Kind() }
func (e stackBaseError) Retryable() Ternary        { return e.base.Retryable() }
func (e stackBaseError) Details() attrs.Attributes { return e.base.Details() }
func (e stackBaseError) StackFrames() []string     { return []string{"test.lua:2"} }

func TestBuildChainUsesNearestMetadata(t *testing.T) {
	inner := NewRich(Unavailable, "down").WithRetryable(True).WithDetails(map[string]any{"layer": "inner"})
	outer := E(Invalid, "bad input", False, attrs.NewBagFrom(map[string]any{"layer": "outer"}), inner)
	for _, tc := range []struct {
		err  error
		name string
	}{
		{outer, "direct"},
		{fmt.Errorf("context: %w", outer), "wrapped"},
		{errors.Join(outer, errors.New("cleanup")), "joined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := BuildChain(tc.err).Root()
			if root.Kind != string(Invalid) || root.Retryable == nil || *root.Retryable || root.Details["layer"] != "outer" {
				t.Fatalf("root = %+v", root)
			}
		})
	}
	stacked := stackBaseError{New(Invalid, "bad")}
	root := BuildChain(stacked).Root()
	if len(root.Stack) != 1 || root.Stack[0] != "test.lua:2" {
		t.Fatalf("stack = %v", root.Stack)
	}
}
