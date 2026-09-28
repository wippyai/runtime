// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"os"
	"syscall"
)

func main() {
	visible, err := os.ReadFile(os.Getenv("VISIBLE"))
	if err != nil || string(visible) != "visible" {
		os.Exit(41)
	}
	if _, err := os.ReadFile(os.Getenv("HIDDEN")); err == nil {
		os.Exit(42)
	}
	if _, err := os.ReadFile("/etc/passwd"); err == nil {
		os.Exit(43)
	}
	// PID 1 is the trusted namespace supervisor. The application runs as a
	// normal child so default-action signals retain ordinary Unix semantics.
	if os.Getpid() == 1 {
		os.Exit(44)
	}
	for _, family := range []int{syscall.AF_INET, syscall.AF_INET6, syscall.AF_UNIX} {
		fd, err := syscall.Socket(family, syscall.SOCK_STREAM, 0)
		if err == nil {
			_ = syscall.Close(fd)
			os.Exit(45)
		}
	}
	fmt.Println("confined")
}
