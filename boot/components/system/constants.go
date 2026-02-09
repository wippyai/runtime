package system

import "github.com/wippyai/runtime/api/boot"

const (
	// FilesystemName is a System component name
	FilesystemName  boot.Name = "filesystem"
	EnvironmentName boot.Name = "env"
	NetworkName     boot.Name = "network"
	ResourcesName   boot.Name = "resources"
	InterceptorName boot.Name = "interceptor"
	FunctionsName   boot.Name = "functions"
	ContractsName   boot.Name = "contracts"
	ClusterName     boot.Name = "cluster"

	// ClusterEnabled is a Cluster configuration key
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
