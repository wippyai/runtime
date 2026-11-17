// SPDX-License-Identifier: MPL-2.0

package time

import (
	stdtime "time"
)

// Time wraps time.Time for Lua userdata.
type Time struct {
	time    stdtime.Time
	utcView bool
}

// wall returns the calendar representation. Values from now retain their
// monotonic reading for comparisons and subtraction.
func (t *Time) wall() stdtime.Time {
	if t.utcView {
		return t.time.UTC()
	}
	return t.time
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
var utcLocation = &Location{location: stdtime.UTC}
