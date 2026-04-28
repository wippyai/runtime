// SPDX-License-Identifier: MPL-2.0

package health

import "errors"

var (
	errDisabled   = errors.New("liveness: check disabled")
	errCheckPanic = errors.New("liveness: check panicked")
)
