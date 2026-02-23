-- SPDX-License-Identifier: MPL-2.0

type InnerID = string @min_len(2)
type Flag = "hot" | "cold" | "warm"
type Flags = {Flag} @min_len(1) @max_len(3)
type Inner = {id: InnerID, flags: Flags}

local function make_inner(id: string, flags: Flags): Inner
	return ({id = id, flags = flags} as Inner)
end

return {
	InnerID = InnerID,
	Flag = Flag,
	Flags = Flags,
	Inner = Inner,
	make_inner = make_inner
}
