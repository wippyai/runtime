local inner = require("inner")

type Label = string @min_len(1) @max_len(8)
type Outer = {inner: inner.Inner, label: Label, meta?: {[string]: string}}
type OuterList = {Outer} @min_len(1)

local function wrap(val: inner.Inner, label: string): Outer
	return ({inner = val, label = label} as Outer)
end

return {
	Inner = inner.Inner,
	Label = Label,
	Outer = Outer,
	OuterList = OuterList,
	wrap = wrap
}
