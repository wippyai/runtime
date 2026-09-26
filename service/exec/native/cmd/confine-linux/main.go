// SPDX-License-Identifier: MPL-2.0

//go:build linux

// Command confine-linux is the minimal Linux exec trampoline. It is built as
// a separate image and packed into the runtime executable; none of the main
// runtime's package initializers run in the pre-policy child.
package main

import (
	"errors"
	"os"

	"github.com/wippyai/runtime/service/exec/native/internal/confinement/linux"
)

func main() {
	if err := linux.RunHelper(); err != nil {
		var target *linux.TargetExitError
		if errors.As(err, &target) {
			os.Exit(target.Code)
		}
		os.Exit(111)
	}
}
