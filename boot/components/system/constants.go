package system

import "github.com/wippyai/runtime/api/boot"

const (
	// System component names
	FilesystemName      boot.Name = "filesystem"
	EnvironmentName     boot.Name = "env"
	ResourcesName       boot.Name = "resources"
	InterceptorName     boot.Name = "interceptor"
	FunctionsName       boot.Name = "functions"
	ProcessName         boot.Name = "process"
	ProcessTopologyName boot.Name = "process-topology"
	ContractsName       boot.Name = "contracts"
	ClusterName         boot.Name = "cluster"

	// Cluster configuration keys
	ClusterEnabled              boot.Name = "enabled"
	ClusterNodeName             boot.Name = "name"
	ClusterInternodeBindAddr    boot.Name = "internode.bind_addr"
	ClusterInternodeBindPort    boot.Name = "internode.bind_port"
	ClusterInternodeAutoPort    boot.Name = "internode.auto_port"
	ClusterMembershipBindAddr   boot.Name = "membership.bind_addr"
	ClusterMembershipBindPort   boot.Name = "membership.bind_port"
	ClusterMembershipJoin       boot.Name = "membership.join_addrs"
	ClusterMembershipSecret     boot.Name = "membership.secret_key"
	ClusterMembershipSecretFile boot.Name = "membership.secret_file"
	ClusterMembershipAdvertise  boot.Name = "membership.advertise_addr"
)
