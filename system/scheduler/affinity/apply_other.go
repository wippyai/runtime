// SPDX-License-Identifier: MPL-2.0

//go:build !linux

package affinity

// pin is a no-op on platforms without CPU affinity support.
func pin(Set) error { return nil }
