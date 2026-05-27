// SPDX-License-Identifier: MPL-2.0

// Package queue holds the registry-kind identifier and errors for
// queue.queue entries. The entry shape is queueapi.Config directly.
package queue

import (
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
)

// Kind identifies queue entries in the registry.
const Kind registry.Kind = "queue.queue"

// Config aliases the core queue config so DecodeEntryConfig[queuecfg.Config]
// keeps working while everything else reads queueapi.Config directly.
type Config = queueapi.Config
