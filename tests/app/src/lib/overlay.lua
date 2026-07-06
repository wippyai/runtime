-- SPDX-License-Identifier: MPL-2.0

-- Overlay library granted privileged access to the funcs infrastructure.
-- It exposes a narrow API; callers that import the overlay must not gain
-- direct access to funcs themselves.
local funcs = require("funcs")

local overlay = {}

-- probe reports the type of the funcs binding visible to the overlay.
-- Only code that legitimately imported funcs (this library) sees a table.
function overlay.probe()
	return type(funcs)
end

return overlay
