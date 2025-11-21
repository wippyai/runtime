package service

import "github.com/wippyai/runtime/api/boot"

const (
	HTTPName              boot.ComponentName = "http"
	TerminalName          boot.ComponentName = "terminal"
	ProcessSupervisorName boot.ComponentName = "process_supervisor"
	ProcessFuncName       boot.ComponentName = "process_func"
	EphemeralHostName     boot.ComponentName = "ephemeral_host"
	NativeExecName        boot.ComponentName = "exec"
	TemplateName          boot.ComponentName = "template"
	EnvName               boot.ComponentName = "envstore"
	YAMLPolicyName        boot.ComponentName = "policy"
	DirectoryName         boot.ComponentName = "directory"
	ContractName          boot.ComponentName = "contract"
	InterceptorRetryName  boot.ComponentName = "interceptor-retry"
)
