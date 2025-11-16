package core

import "github.com/wippyai/runtime/api/boot"

const (
	// Core component names
	EventBusName    boot.ComponentName = "eventbus"
	PIDGenName      boot.ComponentName = "pidgen"
	TranscoderName  boot.ComponentName = "transcoder"
	LogManagerName  boot.ComponentName = "logmanager"
	SecurityName    boot.ComponentName = "security"
	RegistryName    boot.ComponentName = "registry"
	FinderName      boot.ComponentName = "finder"
	SupervisorName  boot.ComponentName = "supervisor"
	ProfilerName    boot.ComponentName = "profiler"
	LoaderName      boot.ComponentName = "loader"
	EventRouterName boot.ComponentName = "eventrouter"

	// Finder configuration keys
	FinderQueryCacheSize boot.ConfigKey = "query_cache_size"
	FinderRegexCacheSize boot.ConfigKey = "regex_cache_size"

	// Registry configuration keys
	RegistryEnableHistory boot.ConfigKey = "enable_history"
)
