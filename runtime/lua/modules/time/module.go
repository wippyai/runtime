// SPDX-License-Identifier: MPL-2.0

package time

import (
	stdtime "time"
)

// Time wraps time.Time for Lua userdata.
type Time struct {
	time stdtime.Time
}

// Duration wraps time.Duration for Lua userdata.
type Duration struct {
	duration stdtime.Duration
}

// Location wraps time.Location for Lua userdata.
type Location struct {
	location *stdtime.Location
}

// Pre-created location values (cached, immutable)
var (
	utcLocation   = &Location{location: stdtime.UTC}
	localLocation = &Location{location: stdtime.Local}
)
