// SPDX-License-Identifier: MPL-2.0
package wasm

import "time"

// EffectiveSocketTimeout converts the configured operation timeout without
// allowing an unchecked host configuration to overflow into a negative duration.
func (c LimitsConfig) EffectiveSocketTimeout() time.Duration {
	milliseconds := int64(c.EffectiveSocketTimeoutMS())
	const maxDuration = time.Duration(1<<63 - 1)
	if milliseconds > int64(maxDuration/time.Millisecond) {
		return maxDuration
	}
	return time.Duration(milliseconds) * time.Millisecond
}
