// SPDX-License-Identifier: MPL-2.0

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
	RaftName        boot.Name = "raft"
	GlobalRegName   boot.Name = "globalreg"
	EventualRegName boot.Name = "eventualreg"
	PGName          boot.Name = "pg"

	// RaftEnabled is a Raft configuration key
	RaftEnabled           boot.Name = "enabled"
	RaftDataDir           boot.Name = "data_dir"
	RaftBindAddr          boot.Name = "bind_addr"
	RaftBindPort          boot.Name = "bind_port"
	RaftAutoPort          boot.Name = "auto_port"
	RaftAdvertiseAddr     boot.Name = "advertise_addr"
	RaftBootstrap         boot.Name = "bootstrap"
	RaftMaxVoters         boot.Name = "max_voters"
	RaftMaxStandbys       boot.Name = "max_standbys"
	RaftReconcileDebounce boot.Name = "reconcile_debounce"
	RaftReconcileTimeout  boot.Name = "reconcile_timeout"
	RaftSnapshotThreshold boot.Name = "snapshot_threshold"
	RaftSnapshotInterval  boot.Name = "snapshot_interval"
	RaftSnapshotRetain    boot.Name = "snapshot_retain"
	RaftTrailingLogs      boot.Name = "trailing_logs"
	RaftMaxAppendEntries  boot.Name = "max_append_entries"
	RaftHeartbeatTimeout  boot.Name = "heartbeat_timeout"
	RaftElectionTimeout   boot.Name = "election_timeout"
	RaftCommitTimeout     boot.Name = "commit_timeout"
	// RaftLeaderProbeInterval / RaftLeaderProbeGrace tune the globalreg
	// leader-reachability monitor that gates name-readiness.
	RaftLeaderProbeInterval boot.Name = "leader_probe_interval"
	RaftLeaderProbeGrace    boot.Name = "leader_probe_grace"

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
	ClusterRaftEligible         boot.Name = "raft.eligible"
	ClusterRaftPriority         boot.Name = "raft.priority"
	ClusterFailureDomain        boot.Name = "failure_domain"
)
