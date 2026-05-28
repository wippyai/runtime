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
	// with sensible defaults.
	//
	// raft.role is the primary server/client knob (Consul/Nomad style):
	//   "server" (default) — runs a raft Node, participates in bootstrap
	//                        and can be elected voter/standby.
	//   "client"           — pure gossip + dissem routing, no raft Node.
	// raft.enabled is the low-level on/off; a node runs raft only when
	// enabled AND role != "client", so the two compose without conflict
	// (either set to off yields a client).
	ClusterRaftEnabled             boot.Name = "raft.enabled"
	ClusterRaftRole                boot.Name = "raft.role"
	ClusterRaftEligible            boot.Name = "raft.eligible"
	ClusterRaftPriority            boot.Name = "raft.priority"
	// BootstrapExpect: the expected size of the initial quorum (Consul/Nomad
	// pattern). All initial nodes ship the same number and join gossip; once
	// that many raft-eligible peers are stably visible they all derive the
	// same sorted server list and call BootstrapCluster with it. Nodes
	// joining a running cluster see existing peers as raft_status=in and
	// skip bootstrap entirely — the leader's reconciler adds them.
	//   0 -> never self-bootstrap (joining an existing cluster)
	//   1 -> single-node mode; bootstrap immediately with self
	//   N -> wait for N alive eligible peers, then form
	ClusterRaftBootstrapExpect boot.Name = "raft.bootstrap_expect"
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
