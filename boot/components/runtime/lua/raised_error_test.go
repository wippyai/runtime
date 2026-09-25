// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"errors"
	"testing"

	glua "github.com/wippyai/go-lua"
	apierror "github.com/wippyai/runtime/api/error"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
)

func TestRaisedErrorMetadataExtractorAndChain(t *testing.T) {
	inner := glua.NewError("inner").WithKind(glua.Unavailable).WithRetryable(true).WithDetails(map[string]any{"layer": "inner"})
	outer := glua.WrapError(inner, "context").WithKind(glua.Invalid).WithRetryable(false).WithDetails(map[string]any{"layer": "outer"})
	converted := runtimelua.ConvertExecutionError(nil, &glua.ApiError{Type: glua.ApiErrorRun, Object: outer})
	metadata := extractLuaErrorMetadata(converted)
	if metadata == nil || metadata.Kind != glua.Invalid || metadata.Retryable == nil || *metadata.Retryable || metadata.Details["layer"] != "outer" {
		t.Fatalf("metadata = %+v", metadata)
	}
	chain := apierror.BuildChain(converted)
	if len(chain.Errors) != 2 || chain.Errors[0].Kind != string(apierror.Invalid) || chain.Errors[1].Kind != string(apierror.Unavailable) || chain.Errors[1].Retryable == nil || !*chain.Errors[1].Retryable {
		t.Fatalf("chain = %+v", chain)
	}
	restored := apierror.FromChain(chain)
	var cause *apierror.RichError
	if restored.Kind() != apierror.Invalid || !errors.As(restored.Unwrap(), &cause) || cause.Kind() != apierror.Unavailable {
		t.Fatalf("restored = %v", restored)
	}
}
