// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/system"
	kvstore "github.com/wippyai/runtime/service/store/kv"
)

// KVRaftStore registers the store.kv.raft kind, bound to the shared raft kv
// engine exposed by the raft boot component. When raft is disabled the engine
// is nil and the manager reports the kind as unavailable.
func KVRaftStore() boot.Component {
	return boot.New(boot.P{
		Name:      KVRaftStoreName,
		DependsOn: []boot.Name{system.RaftName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			engine := system.GetKVRaftEngine(ctx)
			manager := kvstore.NewRaftManager(engine, bus, dtt, logger.Named("kv.raft"))
			handlers.RegisterListener("store.kv.raft", manager)
			return ctx, nil
		},
	})
}
