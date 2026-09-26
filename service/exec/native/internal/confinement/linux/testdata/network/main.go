// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func main() {
	// An omitted fs policy is genuinely unrestricted; network-only
	// confinement must not accidentally install the metadata seccomp rules.
	probe := filepath.Join(os.Getenv("OUTSIDE"), "network-only-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0600); err != nil {
		os.Exit(70)
	}
	defer os.Remove(probe)
	if err := os.Chmod(probe, 0640); err != nil {
		os.Exit(71)
	}
	if os.Getenv("TMPDIR") != "" {
		os.Exit(72)
	}
	for _, family := range []int{syscall.AF_INET, syscall.AF_INET6, syscall.AF_UNIX} {
		fd, err := syscall.Socket(family, syscall.SOCK_STREAM, 0)
		if err == nil {
			_ = syscall.Close(fd)
			os.Exit(73)
		}
	}
	fmt.Println("network-only-ok")
}
