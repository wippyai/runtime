// SPDX-License-Identifier: MPL-2.0
package host

import (
	"context"
	"errors"
	"github.com/wippyai/runtime/api/relay"
	hostapi "github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
	"time"
)

// Recognized control must never fall through to application delivery, including
// unsupported/unversioned representations. The topology receiver performs full
// validation; this fast check leaves ordinary topics on the scheduler path.
func isRemoteMonitorControl(pkg *relay.Package) bool {
	if pkg == nil {
		return false
	}
	for _, message := range pkg.Messages {
		if message == nil || message.Topic != topology.TopicEvents {
			continue
		}
		for _, body := range message.Payloads {
			switch value := body.Data().(type) {
			case *topology.MonitorRequestEvent, *topology.MonitorReleaseEvent:
				return true
			case map[string]any:
				kind, _ := value["kind"].(string)
				if kind == topology.MonitorRequest || kind == topology.MonitorRelease {
					return true
				}
			}
		}
	}
	return false
}

func configuredRemoteMonitorLimit(cfg *hostapi.EntryConfig) int {
	if cfg == nil || cfg.HostConfig.RemoteMonitorLimit == 0 {
		return hostapi.DefaultRemoteMonitorLimit
	}
	return cfg.HostConfig.RemoteMonitorLimit
}

func configuredMonitorReplies(cfg *hostapi.EntryConfig) (int, time.Duration) {
	capacity, timeout := hostapi.DefaultRemoteMonitorReplies, hostapi.DefaultRemoteMonitorReplyTimeout
	if cfg != nil {
		if cfg.HostConfig.RemoteMonitorReplies != 0 {
			capacity = cfg.HostConfig.RemoteMonitorReplies
		}
		if cfg.HostConfig.RemoteMonitorReplyTimeout != 0 {
			timeout = cfg.HostConfig.RemoteMonitorReplyTimeout
		}
	}
	return capacity, timeout
}

// sendRemoteMonitor reserves reply ownership before mutating topology. Network
// delivery runs outside the ingress reader and is joined by host shutdown.
func (h *Host) sendRemoteMonitor(pkg *relay.Package) error {
	h.lifecycleMu.Lock()
	if h.shutdown.Load() || !h.running.Load() {
		h.lifecycleMu.Unlock()
		return ErrHostShuttingDown
	}
	receiver, ok := topology.GetTopology(h.ctx).(interface {
		PrepareRemoteMonitorReply(*relay.Package, int) (bool, *relay.Package, error)
	})
	if !ok {
		h.lifecycleMu.Unlock()
		return errors.New("host topology does not support remote monitor replies")
	}
	sender, ok := relay.GetRouter(h.ctx).(relay.ContextSender)
	if !ok {
		h.lifecycleMu.Unlock()
		return errors.New("remote monitor replies require cancellable relay")
	}
	select {
	case h.monitorReplies <- struct{}{}:
	default:
		h.lifecycleMu.Unlock()
		return errors.New("remote monitor reply capacity exhausted")
	}
	h.monitorWorkers.Add(1)
	ctx := h.monitorCtx
	h.lifecycleMu.Unlock()
	release := func() { <-h.monitorReplies; h.monitorWorkers.Done() }
	handled, reply, err := receiver.PrepareRemoteMonitorReply(pkg, h.remoteMonitorLimit)
	if err != nil || !handled || reply == nil {
		if reply != nil {
			relay.ReleasePackage(reply)
		}
		release()
		if err != nil {
			return err
		}
		return errors.New("unsupported remote monitor control representation")
	}
	relay.ReleasePackage(pkg)
	go func() {
		defer release()
		bounded, cancel := context.WithTimeout(ctx, h.monitorReplyTimeout)
		defer cancel()
		if err := sender.SendContext(bounded, reply); err != nil {
			relay.ReleasePackage(reply)
			h.log.Debug("remote monitor reply not delivered", zap.Error(err))
		}
	}()
	return nil
}
