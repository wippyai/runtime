package main

import (
	"fmt"
	"os"
	"runtime"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/fatih/color"
	"github.com/wippyai/runtime/cmd/wippy/cmd"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	sqlitevec.Auto()

	runtime.GOMAXPROCS(runtime.NumCPU())

	if err := cmd.Execute(); err != nil {
		if cmd.IsConsoleMode() {
			red := color.New(color.FgRed, color.Bold)
			_, _ = red.Fprint(os.Stderr, "Error: ")
			_, _ = fmt.Fprintln(os.Stderr, err)
		} else {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}
