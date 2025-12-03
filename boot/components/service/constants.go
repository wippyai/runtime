package service

import "github.com/wippyai/runtime/api/boot"

const (
	HTTPName              boot.Name = "http"
	Terminal2Name         boot.Name = "terminal2"
	ProcessSupervisorName boot.Name = "process_supervisor"
	ProcessFuncName       boot.Name = "process_func"
	EphemeralHostName     boot.Name = "ephemeral_host"
	EphemeralHost2Name    boot.Name = "ephemeral_host2"
	NativeExecName        boot.Name = "exec"
	TemplateName          boot.Name = "template"
	YAMLPolicyName        boot.Name = "policy"
	DirectoryName         boot.Name = "directory"
	ContractName          boot.Name = "contract"
	InterceptorRetryName  boot.Name = "interceptor-retry"
)
