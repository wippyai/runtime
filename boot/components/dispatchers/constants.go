// SPDX-License-Identifier: MPL-2.0

package dispatchers

import "github.com/wippyai/runtime/api/boot"

const (
	// DispatcherName is the Dispatcher parent dependency
	DispatcherName boot.Name = "dispatcher"

	// ClockDispatcherName is a System dispatcher
	ClockDispatcherName    boot.Name = "dispatcher.clock"
	ContractDispatcherName boot.Name = "dispatcher.contract"
	FuncDispatcherName     boot.Name = "dispatcher.func"
	ProcessDispatcherName  boot.Name = "dispatcher.process"
	SecurityDispatcherName boot.Name = "dispatcher.security"

	// CloudStorageDispatcherName is a Service dispatcher
	CloudStorageDispatcherName boot.Name = "dispatcher.cloudstorage"
	HTTPDispatcherName         boot.Name = "dispatcher.http"
	WSDispatcherName           boot.Name = "dispatcher.ws"
	ExecDispatcherName         boot.Name = "dispatcher.exec"
	StreamDispatcherName       boot.Name = "dispatcher.stream"
	TTYDispatcherName          boot.Name = "dispatcher.tty"
	SQLDispatcherName          boot.Name = "dispatcher.sql"
	EventsDispatcherName       boot.Name = "dispatcher.events"
	SocketDispatcherName       boot.Name = "dispatcher.socket"
	PGDispatcherName           boot.Name = "dispatcher.pg"
)
