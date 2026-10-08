// SPDX-License-Identifier: MPL-2.0

package process

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/process"
)

var lifecycleBenchmarkResult []lua.LValue

func BenchmarkLifecycleYieldResult(b *testing.B) {
	configureAPIErrorMetadataExtractor(b)
	for name, handle := range map[string]func(*lua.LState, any, error) []lua.LValue{
		"cancel":    (&CancelYield{}).HandleResult,
		"terminate": (&TerminateYield{}).HandleResult,
	} {
		b.Run(name, func(b *testing.B) {
			for _, tc := range []struct {
				err       error
				name      string
				kind      lua.Kind
				retryable lua.Ternary
			}{
				{name: "success"},
				{name: "not_found", err: process.ErrProcessNotFound, kind: lua.NotFound, retryable: lua.TernaryFalse},
				{name: "retryable", err: apierror.New(apierror.Unavailable, "try later").WithRetryable(apierror.True), kind: lua.Unavailable, retryable: lua.TernaryTrue},
				{name: "unclassified", err: errors.New("ordinary failure"), kind: lua.Unknown, retryable: lua.TernaryUnknown},
			} {
				b.Run(tc.name, func(b *testing.B) {
					l := lua.NewState()
					defer l.Close()
					if tc.err != nil {
						// Boot installs this extractor in production. Verify that the
						// benchmark actually exercises classified vs fallback paths.
						err := lua.WrapErrorWithLua(l, tc.err, "benchmark preflight")
						require.Equal(b, tc.kind, err.Kind())
						require.Equal(b, tc.retryable, err.Retryable())
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						lifecycleBenchmarkResult = handle(l, nil, tc.err)
					}
				})
			}
		})
	}
}
