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

// KVCRDTStore registers the store.kv.crdt kind, bound to the shared gossip-CRDT
// kv engine exposed by the kv-crdt boot component. When the cluster is disabled
// the engine is nil and the manager reports the kind as unavailable.
func KVCRDTStore() boot.Component {
	return boot.New(boot.P{
		Name:      KVCRDTStoreName,
		DependsOn: []boot.Name{system.KVCRDTName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			engine := system.GetKVCRDTEngine(ctx)
			manager := kvstore.NewCRDTManager(engine, bus, dtt, logger.Named("kv.crdt"))
			handlers.RegisterListener("store.kv.crdt", manager)
			return ctx, nil
		},
	})
}
