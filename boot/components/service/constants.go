package service

import "github.com/wippyai/runtime/api/boot"

const (
	HTTPName              boot.ComponentName = "http"
	Terminal2Name         boot.ComponentName = "terminal2"
	ProcessSupervisorName boot.ComponentName = "process_supervisor"
	ProcessFuncName       boot.ComponentName = "process_func"
	EphemeralHostName     boot.ComponentName = "ephemeral_host"
	EphemeralHost2Name    boot.ComponentName = "ephemeral_host2"
	NativeExecName        boot.ComponentName = "exec"
	TemplateName          boot.ComponentName = "template"
	YAMLPolicyName        boot.ComponentName = "policy"
	DirectoryName         boot.ComponentName = "directory"
	ContractName          boot.ComponentName = "contract"
	InterceptorRetryName  boot.ComponentName = "interceptor-retry"
)
