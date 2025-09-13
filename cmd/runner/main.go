package main

import (
	"os"
	"runtime"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/ponyruntime/pony/cmd/runner/cmd"

	// supported dbs
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// Initialize sqlite-vec extension
	sqlitevec.Auto()

	// Set max procs based on CPU quota if running in container
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Execute Cobra CLI
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
