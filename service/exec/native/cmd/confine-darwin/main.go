// SPDX-License-Identifier: MPL-2.0

//go:build darwin && cgo

// Command confine-darwin is the minimal Seatbelt trampoline. It receives a
// verified policy over inherited descriptors, enters a descriptor-pinned cwd,
// installs Seatbelt, acknowledges readiness, and replaces itself with target.
package main

import (
	"fmt"
	"os"

	confinedarwin "github.com/wippyai/runtime/service/exec/native/internal/confinement/darwin"
)

func main() {
	if err := confinedarwin.RunHelper(); err != nil {
		fmt.Fprintln(os.Stderr, "confine-darwin:", err)
		os.Exit(111)
	}
}
