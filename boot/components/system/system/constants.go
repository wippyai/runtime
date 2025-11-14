package system

import "github.com/wippyai/runtime/api/boot"

const (
	// System component names
	FilesystemName  boot.ComponentName = "filesystem"
	EnvironmentName boot.ComponentName = "env"
	ResourcesName   boot.ComponentName = "resources"
	InterceptorName boot.ComponentName = "interceptor"
	FunctionsName   boot.ComponentName = "functions"
	ProcessName     boot.ComponentName = "process"
	ContractsName   boot.ComponentName = "contracts"
)
