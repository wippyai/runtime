package core

import "github.com/wippyai/runtime/api/boot"

const (
	// PIDGenName is the name for the PID generator component
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
	// RegistryDispatchInternalKinds configures registry entry kinds that bypass event dispatch.
	RegistryDispatchInternalKinds boot.Name = "dispatch_internal_kinds"

	// RegistryDependencyResolveTimeout configures dependency resolution timeout.
	RegistryDependencyResolveTimeout boot.Name = "dependency_resolve_timeout"
	// RegistryDependencyDownloadTimeout configures dependency download timeout.
	RegistryDependencyDownloadTimeout boot.Name = "dependency_download_timeout"
	// RegistryDependencyLockPath overrides lock file path for dependency installs.
	RegistryDependencyLockPath boot.Name = "dependency_lock_path"
	// RegistryDependencyVendorDir overrides vendor directory for dependency installs.
	RegistryDependencyVendorDir boot.Name = "dependency_vendor_dir"
)
