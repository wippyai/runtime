// SPDX-License-Identifier: MPL-2.0
package kvbacked

import "fmt"

// ConfigureParticipation selects committed inventory for Strong reservations.
// Call after ConfigureStrong and before registering hosts or starting work.
// All naming participants must use the same protocol; this is not a mixed-mode
// migration switch. maxParticipants bounds retained enrollment, not discovery.
// Configuration performs no enrollment or other persistent mutation.
func (s *Service) ConfigureParticipation(maxParticipants int) error {
	if s.strong == nil {
		return fmt.Errorf("participation requires Strong configuration")
	}
	if s.reconciler.Load() != nil || s.ready.Load() {
		return fmt.Errorf("participation must be configured before startup")
	}
	if s.strong.participants != nil {
		return fmt.Errorf("participation is already configured")
	}
	if err := validateParticipantIdentity(s.selfNode, s.strong.incarnation); err != nil {
		return err
	}
	inventory, err := newParticipantInventory(s.engine, s.get, maxParticipants)
	if err != nil {
		return err
	}
	s.strong.participants = inventory
	return nil
}
