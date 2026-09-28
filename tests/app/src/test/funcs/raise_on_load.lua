-- SPDX-License-Identifier: MPL-2.0

error(errors.new({
	message = "bad declaration",
	kind = errors.INVALID,
	retryable = false,
	details = { field = "target", nested = { code = "required" } },
}))
