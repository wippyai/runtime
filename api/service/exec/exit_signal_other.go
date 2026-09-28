// SPDX-License-Identifier: MPL-2.0

//go:build !unix

package exec

import "errors"

// exitSignal reports no signal on platforms whose process wait status has no
// Unix signal semantics.
func exitSignal(err error) int {
	var signaler ExitSignaler
	if errors.As(err, &signaler) {
		return signaler.ExitSignal()
	}
	return 0
}
