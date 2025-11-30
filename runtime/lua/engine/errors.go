package engine

import (
	"errors"
	"fmt"
)

// DeadlockError indicates all coroutines are blocked with no pending operations.
type DeadlockError struct {
	ThreadCount int
	Message     string
}

func (e *DeadlockError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("deadlock: %s (threads=%d)", e.Message, e.ThreadCount)
	}
	return fmt.Sprintf("deadlock: all %d coroutines blocked with no pending operations", e.ThreadCount)
}

// IsDeadlock returns true if err is a DeadlockError.
func IsDeadlock(err error) bool {
	var deadlock *DeadlockError
	return errors.As(err, &deadlock)
}
