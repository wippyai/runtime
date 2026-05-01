// SPDX-License-Identifier: MPL-2.0

package service

import "github.com/wippyai/runtime/api/boot"

const (
	HTTPName              boot.Name = "http"
	Terminal2Name         boot.Name = "terminal2"
	ProcessSupervisorName boot.Name = "process_supervisor"
	ProcessFuncName       boot.Name = "process_func"
	EphemeralHost2Name    boot.Name = "ephemeral_host2"
	NativeExecName        boot.Name = "exec"
	TemplateName          boot.Name = "template"
	ContractName          boot.Name = "contract"
	InterceptorRetryName  boot.Name = "interceptor-retry"
	NetworkServiceName    boot.Name = "network_service"
)

const (
	NetworkStateDir = "state_dir"
	NetworkDefault  = "default_network"
)

const DefaultNetworkStateDir = ".wippy/network"
