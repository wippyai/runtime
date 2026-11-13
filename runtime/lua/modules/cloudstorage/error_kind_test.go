// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"fmt"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	csapi "github.com/wippyai/runtime/api/cloudstorage"
	apierror "github.com/wippyai/runtime/api/error"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
)

func providerError(kind apierror.Kind, retryable apierror.Ternary, code string, sentinel error) error {
	cause := fmt.Errorf("operation error S3: ListObjectsV2, api error %s", code)
	if sentinel != nil {
		cause = fmt.Errorf("%w: %w", sentinel, cause)
	}
	return apierror.E(kind, "list objects", retryable,
		attrs.Bag{"code": code, "status_code": 403, "operation": "ListObjectsV2"}, cause)
}

func TestListObjectsYield_ProviderErrorKinds(t *testing.T) {
	lua.SetErrorMetadataExtractor(runtimelua.ErrorMetadata)
	t.Cleanup(func() { lua.SetErrorMetadataExtractor(nil) })

	cases := []struct {
		err       error
		name      string
		kind      lua.Kind
		code      string
		message   string
		retryable lua.Ternary
	}{
		{
			name: "credentials rejected", kind: lua.PermissionDenied, retryable: lua.TernaryFalse, code: "InvalidAccessKeyId",
			err: providerError(apierror.PermissionDenied, apierror.False, "InvalidAccessKeyId", nil),
		},
		{
			name: "endpoint unreachable", kind: lua.Unavailable, retryable: lua.TernaryTrue, code: "RequestSendError",
			err: providerError(apierror.Unavailable, apierror.True, "RequestSendError", nil),
		},
		{
			name: "bucket missing", kind: lua.NotFound, retryable: lua.TernaryFalse, code: "NoSuchBucket", message: "not_found",
			err: providerError(apierror.NotFound, apierror.False, "NoSuchBucket", csapi.ErrNotFound),
		},
		{
			name: "precondition", kind: lua.Conflict, retryable: lua.TernaryFalse, code: "PreconditionFailed", message: "precondition_failed",
			err: providerError(apierror.Conflict, apierror.False, "PreconditionFailed", csapi.ErrPreconditionFailed),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireListObjectsYield()
			defer ReleaseListObjectsYield(y)

			result := y.HandleResult(l, csapi.ListObjectsResponse{Error: tc.err}, nil)
			luaErr, ok := result[1].(*lua.Error)
			if !ok {
				t.Fatalf("expected *lua.Error, got %T", result[1])
			}
			if luaErr.Kind() != tc.kind {
				t.Errorf("kind = %s, want %s", luaErr.Kind(), tc.kind)
			}
			if luaErr.Retryable() != tc.retryable {
				t.Errorf("retryable = %s, want %s", luaErr.Retryable(), tc.retryable)
			}
			if tc.message != "" && luaErr.Message != tc.message {
				t.Errorf("message = %q, want %q", luaErr.Message, tc.message)
			}
			if got := luaErr.Details()["code"]; got != tc.code {
				t.Errorf("details.code = %v, want %s", got, tc.code)
			}
		})
	}
}
