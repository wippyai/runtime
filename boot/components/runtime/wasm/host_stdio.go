package wasm

import (
	wippystdio "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/stdio"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var stdioAliases = []string{
	wippystdio.StdinNamespace,
	wippystdio.StdoutNamespace,
	wippystdio.StderrNamespace,
	wippystdio.TerminalStdinNamespace,
	wippystdio.TerminalStdoutNamespace,
	wippystdio.TerminalStderrNamespace,
	"wasi:cli/stdin",
	"wasi:cli/stdout",
	"wasi:cli/stderr",
	"wasi:cli/terminal-stdin",
	"wasi:cli/terminal-stdout",
	"wasi:cli/terminal-stderr",
}

func stdioHosts(resources *preview2.ResourceTable) []wasmrt.Host {
	return []wasmrt.Host{
		wippystdio.NewStdioHost(resources),
		wippystdio.NewStdoutHost(resources),
		wippystdio.NewStderrHost(resources),
		wippystdio.NewTerminalStdinHost(),
		wippystdio.NewTerminalStdoutHost(),
		wippystdio.NewTerminalStderrHost(),
	}
}
