package core

import "github.com/wippyai/runtime/api/boot"

const (
	// Core component names
	EventBusName    boot.Name = "eventbus"
	PIDGenName      boot.Name = "pidgen"
	TranscoderName  boot.Name = "transcoder"
	LogManagerName  boot.Name = "logmanager"
	SecurityName    boot.Name = "security"
	RegistryName    boot.Name = "registry"
	FinderName      boot.Name = "finder"
	SupervisorName  boot.Name = "supervisor"
	ProfilerName    boot.Name = "profiler"
	LoaderName      boot.Name = "loader"
	EventRouterName boot.Name = "eventrouter"
	DispatcherName  boot.Name = "dispatcher"

	// Finder configuration keys
	FinderQueryCacheSize boot.Name = "query_cache_size"
	FinderRegexCacheSize boot.Name = "regex_cache_size"

	// Registry configuration keys
	RegistryEnableHistory boot.Name = "enable_history"
	RegistryHistoryType   boot.Name = "history_type"
	RegistryHistoryPath   boot.Name = "history_path"
)
