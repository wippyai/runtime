// SPDX-License-Identifier: MPL-2.0
package topology

import "github.com/wippyai/runtime/api/pid"

// MonitorReferences shares bounded exact-reference admission with native
// providers. Release tombstones persist until the target lifetime is closed.
type MonitorReferences struct{ state *remoteMonitorSet }

func NewMonitorReferences(maximum int) (*MonitorReferences, error) {
	s, err := newRemoteMonitorSet(maximum)
	if err != nil {
		return nil, err
	}
	return &MonitorReferences{state: s}, nil
}
func (s *MonitorReferences) Admit(caller pid.PID, previous, reference string) error {
	return s.state.establish(caller, previous, reference)
}
func (s *MonitorReferences) Release(caller pid.PID, reference string) error {
	return s.state.release(caller, reference)
}
func (s *MonitorReferences) Close() { s.state.close() }
