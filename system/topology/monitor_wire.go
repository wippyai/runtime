// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"errors"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
)

const remoteMonitorVersion uint64 = 1

// A reference identifies one monitor relationship. A release must match the
// installed reference; caller/target alone cannot distinguish a stale release.
// This is native control carried by the existing relay codec, not an app API.
type remoteMonitorControl struct {
	kind           string
	reference      string
	previous       string
	caller, target pid.PID
}

// decodeRemoteMonitor borrows pkg. A recognized but invalid control returns an
// error; it must never fall through into application delivery. Unrelated events
// are left to their existing handlers. Only connection-derived provenance can
// authorize remote control; payload identity is checked against that provenance.
func decodeRemoteMonitor(pkg *relay.Package, local pid.NodeID) (*remoteMonitorControl, error) {
	if pkg == nil {
		return nil, errors.New("nil remote monitor package")
	}
	var control *remoteMonitorControl
	for _, message := range pkg.Messages {
		if message == nil {
			return nil, errors.New("nil remote monitor message")
		}
		if message.Topic != topapi.TopicEvents {
			continue
		}
		for _, body := range message.Payloads {
			fields, ok := body.Data().(map[string]any)
			if !ok {
				continue
			}
			kind, _ := fields["kind"].(string)
			if kind != topapi.MonitorRequest && kind != topapi.MonitorRelease {
				continue
			}
			if len(pkg.Messages) != 1 || len(message.Payloads) != 1 {
				return nil, errors.New("remote monitor control cannot share a package")
			}
			// MsgPack's compact positive integer may decode as signed or unsigned.
			validVersion := false
			switch version := fields["v"].(type) {
			case uint64:
				validVersion = version == remoteMonitorVersion
			case int64:
				validVersion = version == int64(remoteMonitorVersion)
			}
			if !validVersion {
				return nil, errors.New("unsupported remote monitor version")
			}
			ref, ok := fields["ref"].(string)
			if !ok || len(ref) == 0 || len(ref) > 64 {
				return nil, errors.New("invalid remote monitor reference")
			}
			caller, callerOK := fields["caller"].(pid.PID)
			target, targetOK := fields["target"].(pid.PID)
			if !callerOK || !targetOK || local == "" || pkg.ReceivedFrom == "" || caller.Node != pkg.ReceivedFrom || caller.Node == local || !caller.Equal(pkg.Source) || target.Node != local || !target.Equal(pkg.Target) || target.Host == "" || target.UniqID == "" {
				return nil, errors.New("remote monitor identity mismatch")
			}
			expectedFields := 5
			previous := ""
			if kind == topapi.MonitorRequest {
				expectedFields = 6
				var ok bool
				previous, ok = fields["previous"].(string)
				if !ok || len(previous) > 64 || previous == ref {
					return nil, errors.New("invalid remote monitor predecessor")
				}
			}
			if len(fields) != expectedFields {
				return nil, errors.New("unknown remote monitor fields")
			}
			control = &remoteMonitorControl{kind: kind, reference: ref, previous: previous, caller: caller, target: target}
		}
	}
	return control, nil
}
