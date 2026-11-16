// SPDX-License-Identifier: MPL-2.0

package httpclient

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	httpapi "github.com/wippyai/runtime/api/service/http"
)

func TestRequestYieldDeniedResponseIsPermissionDenied(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	out := (&RequestYield{}).HandleResult(l, httpapi.Response{
		Error:        `Get "/next": not allowed: http://127.0.0.1:1/next`,
		Denied:       true,
		DeniedTarget: "http://127.0.0.1:1/next",
	}, nil)

	require.Len(t, out, 2)
	require.Equal(t, lua.LNil, out[0])
	luaErr, ok := out[1].(*lua.Error)
	require.True(t, ok)
	require.Equal(t, lua.PermissionDenied, luaErr.Kind())
	require.Equal(t, lua.TernaryFalse, luaErr.Retryable())
	require.Equal(t, "http://127.0.0.1:1/next", luaErr.Details()["url"])
}

func TestRequestYieldTransportErrorStaysRetryable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	out := (&RequestYield{}).HandleResult(l, httpapi.Response{Error: "connection reset"}, nil)

	luaErr, ok := out[1].(*lua.Error)
	require.True(t, ok)
	require.Equal(t, lua.Internal, luaErr.Kind())
	require.Equal(t, lua.TernaryTrue, luaErr.Retryable())
}

func TestRequestBatchYieldDeniedResponseIsPermissionDenied(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	out := (&RequestBatchYield{}).HandleResult(l, httpapi.BatchResponse{Responses: []httpapi.Response{
		{Error: "not allowed: http://127.0.0.1:1/next", Denied: true, DeniedTarget: "http://127.0.0.1:1/next"},
	}}, nil)

	errs, ok := out[1].(*lua.LTable)
	require.True(t, ok)
	luaErr, ok := errs.RawGetInt(1).(*lua.Error)
	require.True(t, ok)
	require.Equal(t, lua.PermissionDenied, luaErr.Kind())
	require.Equal(t, lua.TernaryFalse, luaErr.Retryable())
}
