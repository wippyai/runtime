// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/system/admin"
	"github.com/wippyai/runtime/system/eventualreg"
	"github.com/wippyai/runtime/system/globalreg"
	sysraft "github.com/wippyai/runtime/system/raft"
	"go.uber.org/zap"
)

// eventualRegSvcKey is the app-context key holding the EVENTUAL registry
// service. Published by the eventualreg boot component, consumed here so
// the admin server can expose its digest + CV for gossip-convergence
// checks.
var eventualRegSvcKey = &ctxapi.Key{Name: "eventualreg.service"}

// Admin returns a boot component that serves the read-only chaos/integration
// admin HTTP endpoints. Disabled unless `admin.bind_addr` is set in boot
// config — keeps the surface off in production by default.
//
// All required services (Raft, eventualreg, membership) are published into
// app context by their owning components — this component depends on Raft
// so the keys are present by Load. KVRaft is optional; missing KVRaft
// returns 503 from /admin/raft/status?group=kv only.
func Admin() boot.Component {
	var server *http.Server
	var logger *zap.Logger

	return boot.New(boot.P{
		Name:      AdminName,
		DependsOn: []boot.Name{RaftName, EventualRegName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger = logapi.GetLogger(ctx).Named("admin")

			cfg := boot.GetConfig(ctx)
			if cfg == nil {
				return ctx, nil
			}
			sub := cfg.Sub(AdminName)
			if sub == nil {
				logger.Debug("admin disabled (no admin config)")
				return ctx, nil
			}
			bindAddr := sub.GetString(AdminBindAddr, "")
			if bindAddr == "" {
				logger.Debug("admin disabled (admin.bind_addr empty)")
				return ctx, nil
			}

			ac := ctxapi.AppFromContext(ctx)
			if ac == nil {
				return ctx, fmt.Errorf("admin: app context unavailable")
			}
			globalRaft, _ := ac.Get(raftNodeKey).(*sysraft.Node)
			kvRaft, _ := ac.Get(kvRaftNodeKey).(*sysraft.Node)
			eventualReg, _ := ac.Get(eventualRegSvcKey).(*eventualreg.Service)
			globalReg, _ := ac.Get(globalRegSvcKey).(*globalreg.Service)
			membership := clusterapi.GetMembership(ctx)

			if globalRaft == nil || eventualReg == nil || membership == nil {
				return ctx, fmt.Errorf("admin: missing required service (raft=%v eventualreg=%v membership=%v)",
					globalRaft != nil, eventualReg != nil, membership != nil)
			}

			mux := admin.NewMux(admin.Deps{
				GlobalRaft:  globalRaft,
				KVRaft:      kvRaft,
				EventualReg: eventualReg,
				GlobalReg:   globalReg,
				Membership:  membership,
				Logger:      logger,
			})
			server = &http.Server{
				Addr:              bindAddr,
				Handler:           mux,
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       15 * time.Second,
				WriteTimeout:      30 * time.Second,
				IdleTimeout:       60 * time.Second,
			}
			logger.Info("admin server configured", zap.String("bind_addr", bindAddr))
			return ctx, nil
		},
		Start: func(_ context.Context) error {
			if server == nil {
				return nil
			}
			go func() {
				logger.Info("starting admin HTTP server", zap.String("addr", server.Addr))
				if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.Error("admin server failed", zap.Error(err))
				}
			}()
			return nil
		},
		Stop: func(ctx context.Context) error {
			if server == nil {
				return nil
			}
			logger.Info("stopping admin server")
			shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("admin: shutdown: %w", err)
			}
			return nil
		},
	})
}
