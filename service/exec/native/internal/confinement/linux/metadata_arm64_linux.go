// SPDX-License-Identifier: MPL-2.0

//go:build linux && arm64

package linux

// arm64 entered Linux after these legacy syscall numbers were removed.
func legacyMetadataSyscalls() []uint32 { return nil }
