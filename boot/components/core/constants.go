package core

import "github.com/wippyai/runtime/api/boot"

const (
	// Core component names
	PIDGenName         boot.Name = "pidgen"
	SecurityName       boot.Name = "security"
	SecurityPolicyName boot.Name = "security.policy"
	RegistryName       boot.Name = "registry"
	FinderName         boot.Name = "finder"
	SupervisorName     boot.Name = "supervisor"
	ProfilerName       boot.Name = "profiler"
	LoaderName         boot.Name = "loader"
	EventRouterName    boot.Name = "eventrouter"
	DispatcherName     boot.Name = "dispatcher"

	// FinderQueryCacheSize is a Finder configuration key
	FinderQueryCacheSize boot.Name = "query_cache_size"
	FinderRegexCacheSize boot.Name = "regex_cache_size"

	// RegistryEnableHistory is a Registry configuration key
	RegistryEnableHistory boot.Name = "enable_history"
	RegistryHistoryType   boot.Name = "history_type"
	RegistryHistoryPath   boot.Name = "history_path"
)
