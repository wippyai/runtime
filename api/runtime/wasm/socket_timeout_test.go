// SPDX-License-Identifier: MPL-2.0
package wasm

import (
	"testing"
	"time"
)

func TestEffectiveSocketTimeout(t *testing.T) {
	for _, tc := range []struct {
		milliseconds int
		want         time.Duration
	}{
		{0, time.Duration(DefaultSocketTimeoutMS) * time.Millisecond},
		{125, 125 * time.Millisecond},
		{-1, time.Duration(DefaultSocketTimeoutMS) * time.Millisecond},
	} {
		if got := (LimitsConfig{SocketTimeoutMS: tc.milliseconds}).EffectiveSocketTimeout(); got != tc.want {
			t.Fatalf("%dms became %v, want %v", tc.milliseconds, got, tc.want)
		}
	}
	// Conversion also stays safe for unchecked programmatic configuration.
	maxInt := int(^uint(0) >> 1)
	if int64(maxInt) > int64((time.Duration(1<<63-1))/time.Millisecond) {
		got := (LimitsConfig{SocketTimeoutMS: maxInt}).EffectiveSocketTimeout()
		if got != time.Duration(1<<63-1) {
			t.Fatalf("overflowing milliseconds became %v", got)
		}
	}
}
