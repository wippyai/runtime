package service

import "github.com/ponyruntime/pony/api/boot"

const (
	// Service component names
	HTTPName               boot.ComponentName = "http"
	SQLName                boot.ComponentName = "sql"
	SQLStoreName           boot.ComponentName = "sqlstore"
	MemStoreName           boot.ComponentName = "memstore"
	TokenStoreName         boot.ComponentName = "tokenstore"
	TerminalName           boot.ComponentName = "terminal"
	ProcessSupervisorName  boot.ComponentName = "process_supervisor"
	ProcessFuncName        boot.ComponentName = "process_func"
	EphemeralHostName      boot.ComponentName = "ephemeral_host"
	NativeExecName         boot.ComponentName = "exec"
	TemplateName           boot.ComponentName = "template"
	EnvName                boot.ComponentName = "envstore"
	YAMLPolicyName         boot.ComponentName = "policy"
	AWSConfigName          boot.ComponentName = "aws_config"
	S3Name                 boot.ComponentName = "s3"
	DirectoryName          boot.ComponentName = "directory"
	ContractName           boot.ComponentName = "contract"
	InterceptorManagerName boot.ComponentName = "interceptor-manager"
	InterceptorOtelName    boot.ComponentName = "interceptor-otel"
	InterceptorRetryName   boot.ComponentName = "interceptor-retry"
)
