// SPDX-License-Identifier: MPL-2.0

package core

import (
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/artifact"
)

const (
	// PIDGenName is the name for the PID generator component
	PIDGenName         boot.Name = "pidgen"
	SecurityName       boot.Name = "security"
	SecurityPolicyName boot.Name = "security.policy"
	ArtifactName       boot.Name = artifact.ConfigName
	RegistryName       boot.Name = "registry"
	FinderName         boot.Name = "finder"
	SupervisorName     boot.Name = "supervisor"
	ProfilerName       boot.Name = "profiler"
	LoaderName         boot.Name = "loader"
	EventRouterName    boot.Name = "eventrouter"
	DispatcherName     boot.Name = "dispatcher"

	// SchedulerName is the .wippy.yaml section for scheduler tuning.
	SchedulerName boot.Name = "scheduler"
	// WASMIsolationName is the scheduler sub-section that reserves cores for WASM.
	WASMIsolationName boot.Name = "wasm_isolation"
	// WASMIsolationEnabled toggles the WASM/actor core partition (default false).
	WASMIsolationEnabled boot.Name = "enabled"
	// WASMIsolationReserved is the number of cores reserved for WASM execution.
	WASMIsolationReserved boot.Name = "reserved_cores"

	// FinderQueryCacheSize is a Finder configuration key
	FinderQueryCacheSize boot.Name = "query_cache_size"
	FinderRegexCacheSize boot.Name = "regex_cache_size"

	// RegistryEnableHistory is a Registry configuration key
	RegistryEnableHistory boot.Name = "enable_history"
	RegistryHistoryType   boot.Name = "history_type"
	RegistryHistoryPath   boot.Name = "history_path"
	RegistryHistoryDSN    boot.Name = "history_dsn"
	RegistryHistorySchema boot.Name = "history_schema"
	// RegistryDispatchInternalKinds configures registry entry kinds that bypass event dispatch.
	RegistryDispatchInternalKinds boot.Name = "dispatch_internal_kinds"

	// RegistryDependencyResolveTimeout configures dependency resolution timeout.
	RegistryDependencyResolveTimeout boot.Name = "dependency_resolve_timeout"
	// RegistryDependencyDownloadTimeout configures dependency download timeout.
	RegistryDependencyDownloadTimeout boot.Name = "dependency_download_timeout"
	// RegistryEventWaitTimeout configures per-operation listener wait timeout in registry runner.
	RegistryEventWaitTimeout boot.Name = "event_wait_timeout"
	// RegistryDependencyLockPath overrides lock file path for dependency installs.
	RegistryDependencyLockPath boot.Name = "dependency_lock_path"
	// RegistryDependencyVendorDir overrides vendor directory for dependency installs.
	RegistryDependencyVendorDir boot.Name = "dependency_vendor_dir"
)
