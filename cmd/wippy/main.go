// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"os"
	"runtime"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/wippyai/runtime/cmd/wippy/cmd"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	sqlitevec.Auto()

	runtime.GOMAXPROCS(runtime.NumCPU())

	if err := cmd.Execute(); err != nil {
		errStr := err.Error()
		if cmd.IsConsoleMode() {
			// Colorize Rust-style type errors
			errStr = cmd.ColorizeTypeError(errStr)
			_, _ = fmt.Fprintln(os.Stderr, errStr)
		} else {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}
