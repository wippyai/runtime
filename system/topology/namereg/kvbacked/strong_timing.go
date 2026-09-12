// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"fmt"
	"time"

	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

// StrongTiming controls waiting and retry policy, not ownership evidence.
// Configure it before publishing or starting the registry.
type StrongTiming struct {
	AckDeadline     time.Duration
	ResultWaitGrace time.Duration
	RetryInterval   time.Duration
}

func DefaultStrongTiming() StrongTiming {
	return StrongTiming{AckDeadline: globalapi.StrongDeadline, ResultWaitGrace: 2 * time.Second, RetryInterval: time.Second}
}

func (s *Service) ConfigureStrongTiming(policy StrongTiming) error {
	if s.strong == nil {
		return fmt.Errorf("Strong timing requires Strong configuration")
	}
	if policy.AckDeadline <= 0 || policy.ResultWaitGrace < 0 || policy.RetryInterval <= 0 {
		return fmt.Errorf("Strong timing requires positive acknowledgement deadline and retry interval, and nonnegative result grace")
	}
	s.strong.deadline = policy.AckDeadline
	s.strong.resultWaitGrace = policy.ResultWaitGrace
	s.strong.retryInterval = policy.RetryInterval
	return nil
}

func strongRegistrationDeadline(ctx context.Context, now time.Time, timeout time.Duration) time.Time {
	deadline := now.Add(timeout)
	if caller, ok := ctx.Deadline(); ok && caller.Before(deadline) {
		return caller
	}
	return deadline
}
