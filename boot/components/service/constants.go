// SPDX-License-Identifier: MPL-2.0

package service

import "github.com/wippyai/runtime/api/boot"

const (
	NetworkServiceName    boot.Name = "network_service"
	HTTPName              boot.Name = "http"
	Terminal2Name         boot.Name = "terminal2"
	ProcessSupervisorName boot.Name = "process_supervisor"
	ProcessFuncName       boot.Name = "process_func"
	EphemeralHost2Name    boot.Name = "ephemeral_host2"
	NativeExecName        boot.Name = "exec"
	TemplateName          boot.Name = "template"
	ContractName          boot.Name = "contract"
	InterceptorRetryName  boot.Name = "interceptor-retry"
)

// Network service config keys, read from the network_service subsection of
// .wippy.yaml. StateDir is the base directory for driver-local state (tsnet
// keys, future I2P session files, etc.); relative paths resolve against
// boot.config_dir. DefaultNetwork is the app-wide overlay fallback applied
// to any task/process that does not pin its own network via options.
const (
	NetworkStateDir        = "state_dir"
	NetworkDefault         = "default_network"
	DefaultNetworkStateDir = ".wippy/net"
)
