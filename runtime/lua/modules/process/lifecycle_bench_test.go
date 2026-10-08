// SPDX-License-Identifier: MPL-2.0

package process

import (
	"errors"
	"testing"

	lua "github.com/wippyai/go-lua"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/process"
)

var lifecycleBenchmarkResult []lua.LValue

func BenchmarkLifecycleYieldResult(b *testing.B) {
	for name, handle := range map[string]func(*lua.LState, any, error) []lua.LValue{
		"cancel":    (&CancelYield{}).HandleResult,
		"terminate": (&TerminateYield{}).HandleResult,
	} {
		b.Run(name, func(b *testing.B) {
			for _, tc := range []struct {
				err  error
				name string
			}{
				{name: "success"},
				{name: "not_found", err: process.ErrProcessNotFound},
				{name: "retryable", err: apierror.New(apierror.Unavailable, "try later").WithRetryable(apierror.True)},
				{name: "unclassified", err: errors.New("ordinary failure")},
			} {
				b.Run(tc.name, func(b *testing.B) {
					l := lua.NewState()
					defer l.Close()
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
