package system

import "github.com/wippyai/runtime/api/boot"

const (
	// System component names
	FilesystemName      boot.ComponentName = "filesystem"
	EnvironmentName     boot.ComponentName = "env"
	ResourcesName       boot.ComponentName = "resources"
	InterceptorName     boot.ComponentName = "interceptor"
	FunctionsName       boot.ComponentName = "functions"
	ProcessName         boot.ComponentName = "process"
	ProcessTopologyName boot.ComponentName = "process-topology"
	ContractsName       boot.ComponentName = "contracts"
	ClusterName         boot.ComponentName = "cluster"

	// Cluster configuration keys
	ClusterEnabled              boot.ConfigKey = "enabled"
	ClusterNodeName             boot.ConfigKey = "name"
	ClusterInternodeBindAddr    boot.ConfigKey = "internode.bind_addr"
	ClusterInternodeBindPort    boot.ConfigKey = "internode.bind_port"
	ClusterInternodeAutoPort    boot.ConfigKey = "internode.auto_port"
	ClusterMembershipBindAddr   boot.ConfigKey = "membership.bind_addr"
	ClusterMembershipBindPort   boot.ConfigKey = "membership.bind_port"
	ClusterMembershipJoin       boot.ConfigKey = "membership.join_addrs"
	ClusterMembershipSecret     boot.ConfigKey = "membership.secret_key"
	ClusterMembershipSecretFile boot.ConfigKey = "membership.secret_file"
	ClusterMembershipAdvertise  boot.ConfigKey = "membership.advertise_addr"
)
