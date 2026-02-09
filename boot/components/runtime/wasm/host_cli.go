package wasm

import (
	wippycli "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/cli"
	wippystdio "github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/stdio"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var cliAliases = []string{
	wippycli.EnvironmentNamespace,
	wippycli.ExitNamespace,
	wippystdio.StdinNamespace,
	wippystdio.StdoutNamespace,
	wippystdio.StderrNamespace,
	wippystdio.TerminalStdinNamespace,
	wippystdio.TerminalStdoutNamespace,
	wippystdio.TerminalStderrNamespace,
	"wasi:cli/environment",
	"wasi:cli/exit",
	"wasi:cli/stdin",
	"wasi:cli/stdout",
	"wasi:cli/stderr",
	"wasi:cli/terminal-stdin",
	"wasi:cli/terminal-stdout",
	"wasi:cli/terminal-stderr",
}

func cliHosts(resources *preview2.ResourceTable) []wasmrt.Host {
	return []wasmrt.Host{
		wippycli.NewEnvironmentHost(),
		wippycli.NewExitHost(),
		wippystdio.NewHost(resources),
		wippystdio.NewStdoutHost(resources),
		wippystdio.NewStderrHost(resources),
		wippystdio.NewTerminalStdinHost(),
		wippystdio.NewTerminalStdoutHost(),
		wippystdio.NewTerminalStderrHost(),
	}
}
