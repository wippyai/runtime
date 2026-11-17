// SPDX-License-Identifier: MPL-2.0

package time

import (
	"testing"
	stdtime "time"

	lua "github.com/wippyai/go-lua"
)

func TestUTCDefaultsAcrossConstructors(t *testing.T) {
	saved := stdtime.Local
	stdtime.Local = stdtime.FixedZone("ENV", 19800)
	t.Cleanup(func() { stdtime.Local = saved })

	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	if err := l.DoString(`
		local now = time.now()
		assert(now:format("Z07:00") == "Z")
		assert(now:in_local():location():string() == "UTC")
		assert(now:add("5ms"):sub(now):nanoseconds() == 5000000)
		assert(now:add("5ms"):format("Z07:00") == "Z")
		assert(time.localtz:string() == "UTC")
		assert(time.load_location("Local"):string() == "UTC")
		assert(time.date(2024, 1, 1, 0, 0, 0, 0):unix() == 1704067200)
		local epoch = time.unix(0, 0)
		assert(epoch:hour() == 0 and epoch:location():string() == "UTC")
		local custom = time.fixed_zone("custom", 3600)
		assert(epoch:in_location(custom):format("Z07:00") == "+01:00")
	`); err != nil {
		t.Fatal(err)
	}
}
