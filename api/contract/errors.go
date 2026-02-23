// SPDX-License-Identifier: MPL-2.0

package contract

import (
	"errors"
)

// Sentinel errors for contract operations.
var (
	ErrInstantiatorNotFound = errors.New("contract instantiator not found in context")
	ErrInstanceNil          = errors.New("contract instance is nil")
	ErrNodeNotFound         = errors.New("relay node not found")
	ErrPIDNotFound          = errors.New("process PID not found in context")
)
