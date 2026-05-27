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
	ClusterFailureDomain        boot.Name = "failure_domain"

	// Raft lives under cluster.raft.*. Enabling cluster auto-enables raft
	// with sensible defaults; set ClusterRaftEnabled=false to opt out.
	ClusterRaftEnabled             boot.Name = "raft.enabled"
	ClusterRaftEligible            boot.Name = "raft.eligible"
	ClusterRaftPriority            boot.Name = "raft.priority"
	ClusterRaftMaxVoters           boot.Name = "raft.max_voters"
	ClusterRaftMaxStandbys         boot.Name = "raft.max_standbys"
	ClusterRaftReconcileDebounce   boot.Name = "raft.reconcile_debounce"
	ClusterRaftReconcileTimeout    boot.Name = "raft.reconcile_timeout"
	ClusterRaftSnapshotThreshold   boot.Name = "raft.snapshot_threshold"
	ClusterRaftSnapshotInterval    boot.Name = "raft.snapshot_interval"
	ClusterRaftSnapshotRetain      boot.Name = "raft.snapshot_retain"
	ClusterRaftTrailingLogs        boot.Name = "raft.trailing_logs"
	ClusterRaftMaxAppendEntries    boot.Name = "raft.max_append_entries"
	ClusterRaftHeartbeatTimeout    boot.Name = "raft.heartbeat_timeout"
	ClusterRaftElectionTimeout     boot.Name = "raft.election_timeout"
	ClusterRaftCommitTimeout       boot.Name = "raft.commit_timeout"
	ClusterRaftLeaderProbeInterval boot.Name = "raft.leader_probe_interval"
	ClusterRaftLeaderProbeGrace    boot.Name = "raft.leader_probe_grace"
)
