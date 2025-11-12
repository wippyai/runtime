package core

import "github.com/ponyruntime/pony/api/boot"

const (
	// Core plugin names
	LoggerName     = "logger"
	EventBusName   = "eventbus"
	PIDGenName     = "pidgen"
	TranscoderName = "transcoder"
	LogManagerName = "logmanager"
	SecurityName   = "security"
	RegistryName   = "registry"
	SupervisorName = "supervisor"
	ProfilerName   = "profiler"

	// Logger configuration keys
	LoggerMode     boot.ConfigKey = "mode"
	LoggerLevel    boot.ConfigKey = "level"
	LoggerEncoding boot.ConfigKey = "encoding"
)
