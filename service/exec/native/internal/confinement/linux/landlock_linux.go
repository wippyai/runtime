// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"fmt"

	"golang.org/x/sys/unix"
)

const minimumLandlockABI = 5

// LandlockABI returns the kernel's Landlock ABI without installing a policy
// in the runtime process. A confined launch requires a separate helper that
// installs its own rules before it executes the target.
func LandlockABI() (int, error) {
	version, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION,
	)
	if errno != 0 {
		return 0, fmt.Errorf("probe Landlock ABI: %w", errno)
	}
	return int(version), nil
}

func requireLandlockABI() error {
	version, err := LandlockABI()
	if err != nil {
		return err
	}
	if version < minimumLandlockABI {
		return fmt.Errorf("landlock ABI %d lacks required filesystem rights", version)
	}
	return nil
}
