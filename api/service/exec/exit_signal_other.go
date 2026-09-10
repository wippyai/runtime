// SPDX-License-Identifier: MPL-2.0

//go:build !unix

package exec

// exitSignal reports no signal on platforms whose process wait status has no
// Unix signal semantics.
func exitSignal(error) int { return 0 }
