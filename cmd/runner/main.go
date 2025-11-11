package main

import (
	"fmt"
	"os"
	"runtime"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/ponyruntime/pony/cmd/runner/cmd"

	// supported dbs
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// Build information - can be set via ldflags during build
// CI/CD uses: -X main.version=$CLEAN_TAG
var (
	version = "dev"
)

func main() {
	// Log startup information immediately to stderr for debugging
	fmt.Fprintf(os.Stderr, "=== WIPPY STARTUP DEBUG ===\n")
	fmt.Fprintf(os.Stderr, "Version: %s\n", version)
	fmt.Fprintf(os.Stderr, "Go version: %s\n", runtime.Version())
	fmt.Fprintf(os.Stderr, "OS: %s\n", runtime.GOOS)
	fmt.Fprintf(os.Stderr, "Arch: %s\n", runtime.GOARCH)
	fmt.Fprintf(os.Stderr, "Command line args: %v\n", os.Args)
	fmt.Fprintf(os.Stderr, "Working directory: %s\n", getWorkingDir())
	fmt.Fprintf(os.Stderr, "===========================\n")

	// Initialize sqlite-vec extension
	sqlitevec.Auto()

	// Set max procs based on CPU quota if running in container
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Execute Cobra CLI
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func getWorkingDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return wd
}
