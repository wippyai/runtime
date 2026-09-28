// SPDX-License-Identifier: MPL-2.0

package main

import "os"

var marker string

func main() {
	if marker == "" {
		os.Exit(90)
	}
	if err := os.WriteFile(marker, []byte("executed"), 0o600); err != nil {
		os.Exit(91)
	}
}
