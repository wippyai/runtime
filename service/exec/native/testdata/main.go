package main

import (
	"errors"
	"fmt"
	"go.uber.org/zap"
	"io"
	"io/fs"
	"os"

	"github.com/ponyruntime/pony/api/service/exec"
	"github.com/ponyruntime/pony/service/exec/native"
)

func main() {
	log := zap.NewNop()
	executor := native.NewNativeExecutor(log, &exec.NativeExecutorConfig{})
	proc, err := executor.NewProcess("echo 'Hello World'", exec.ProcessOptions{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := proc.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, err := io.ReadAll(proc.Stdout())
	if err != nil && !errors.Is(err, fs.ErrClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, string(data))
	os.Exit(0)
}
