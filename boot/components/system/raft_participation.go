// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	sysraft "github.com/wippyai/runtime/cluster/raft"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
)

// configureNamingParticipant replaces the raw sysreg host for both members and
// forwarding clients. An incarnation is a fresh naming lifetime, not a node ID or
// an authorization to replace an existing lifetime. Enrollment refuses that case.
func configureNamingParticipant(ctx context.Context, cfg boot.Config, registry *kvbacked.Service, isLeader func() bool) (*kvbacked.ParticipantEndpoint, error) {
	guard := topology.GetNameGuard(ctx)
	node, router := relay.GetNode(ctx), relay.GetRouter(ctx)
	membership := clusterapi.GetMembership(ctx)
	sender, ok := router.(relay.ContextSender)
	if guard == nil || node == nil || membership == nil || !ok {
		return nil, fmt.Errorf("naming participation requires shared guard, node, membership and cancellable router")
	}
	presence := &localPresenceChecker{ctx: ctx}
	registry.ConfigureStrong(kvbacked.StrongDeps{
		Incarnation: rand.Text(), NameGuard: guard, IsLeader: isLeader,
		LocalRevoker: &localNameRevoker{ctx: ctx},
		LocalConflict: func(name string, _ pid.PID) (pid.PID, bool) {
			if owner, found := presence.LookupLocal(name); found {
				return owner, true
			}
			return presence.LookupEventual(name)
		},
	})
	timing := kvbacked.DefaultStrongTiming()
	if err := registry.ConfigureStrongTiming(kvbacked.StrongTiming{
		AckDeadline:     cfg.GetDuration("raft.naming.strong.ack_deadline", timing.AckDeadline),
		ResultWaitGrace: cfg.GetDuration("raft.naming.strong.result_wait_grace", timing.ResultWaitGrace),
		RetryInterval:   cfg.GetDuration("raft.naming.strong.retry_interval", timing.RetryInterval),
	}); err != nil {
		return nil, err
	}
	results := kvbacked.DefaultStrongResultPolicy()
	resultEntries := cfg.GetInt("raft.naming.strong.results.max_entries", int(results.Entries))
	resultBytes := cfg.GetInt("raft.naming.strong.results.max_bytes", int(results.Bytes))
	resultRecordBytes := cfg.GetInt("raft.naming.strong.results.max_record_bytes", int(results.RecordBytes))
	if resultEntries <= 0 || resultBytes <= 0 || resultRecordBytes <= 0 {
		return nil, fmt.Errorf("Strong result capacity must be positive")
	}
	if err := registry.ConfigureStrongResults(kvbacked.StrongResultPolicy{
		Entries: uint64(resultEntries), Bytes: uint64(resultBytes), RecordBytes: uint64(resultRecordBytes),
		Retention:    cfg.GetDuration("raft.naming.strong.results.retention", results.Retention),
		ReclaimBatch: cfg.GetInt("raft.naming.strong.results.reclaim_batch", min(results.ReclaimBatch, resultEntries)),
	}); err != nil {
		return nil, err
	}
	if err := registry.ConfigureParticipation(cfg.GetInt("raft.naming.max_participants", 4096)); err != nil {
		return nil, err
	}
	cleanupDefaults := kvbacked.DefaultCleanupConfig()
	cleanupPending := cfg.GetInt("raft.naming.cleanup.max_pending", cleanupDefaults.MaxPending)
	config := kvbacked.ParticipantEndpointConfig{
		Cleanup: kvbacked.CleanupConfig{
			MaxPending:    cleanupPending,
			MaxBytes:      cfg.GetInt("raft.naming.cleanup.max_bytes", cleanupDefaults.MaxBytes),
			BatchSize:     cfg.GetInt("raft.naming.cleanup.batch_size", min(cleanupDefaults.BatchSize, cleanupPending)),
			RetryInterval: cfg.GetDuration("raft.naming.cleanup.retry_interval", cleanupDefaults.RetryInterval),
		},
		MaxEntries:            cfg.GetInt("raft.naming.max_entries", 16384),
		MaxValueBytes:         cfg.GetInt("raft.naming.max_value_bytes", 4<<20),
		MaxWireBytes:          cfg.GetInt("raft.naming.max_wire_bytes", 16<<20),
		MaxConcurrentRequests: cfg.GetInt("raft.naming.max_concurrent_requests", 8),
		MaxRedirects:          cfg.GetInt("raft.naming.max_redirects", 4),
		RequestTimeout:        cfg.GetDuration("raft.naming.request_timeout", 5*time.Second),
		RefreshInterval:       cfg.GetDuration("raft.naming.refresh_interval", time.Second),
	}
	endpoint, err := kvbacked.NewParticipantEndpoint(ctx, registry, sender, func(ctx context.Context) (pid.NodeID, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if target, found := sysraft.PickForwardTarget(membership.Nodes(), node.ID()); found {
			return target, nil
		}
		return "", fmt.Errorf("no naming authority available")
	}, config)
	if err != nil {
		return nil, err
	}
	if err := node.RegisterHost(kvbacked.RegistryHostID, endpoint); err != nil {
		_ = endpoint.Stop(context.Background())
		return nil, fmt.Errorf("register naming participant host: %w", err)
	}
	return endpoint, nil
}

// withdrawBootNames must cover the actual registries, not only report a seal.
func withdrawBootNames(ctx context.Context) error {
	local, ok := topology.GetRegistry(ctx).(interface{ WithdrawLocal(context.Context) error })
	if !ok {
		return fmt.Errorf("local registry does not support naming withdrawal")
	}
	if err := local.WithdrawLocal(ctx); err != nil {
		return err
	}
	if eventual := topology.GetEventualRegistry(ctx); eventual != nil {
		withdrawer, ok := eventual.(interface{ WithdrawLocal(context.Context) error })
		if !ok {
			return fmt.Errorf("eventual registry does not support naming withdrawal")
		}
		return withdrawer.WithdrawLocal(ctx)
	}
	return nil
}
