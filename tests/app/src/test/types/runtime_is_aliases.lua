-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

type ID = string @min_len(3)
type UserID = ID
type Role = "admin" | "user" | "guest"
type Roles = {Role} @min_len(1)
type Account = {id: UserID, roles: Roles, note?: string @min_len(2)}

local function main(): boolean
	local id_ok, id_err = UserID:is("abc")
	assert.not_nil(id_ok, "UserID valid should pass")
	assert.is_nil(id_err, "UserID valid should have nil error")

	local id_bad, id_bad_err = UserID:is("a")
	assert.is_nil(id_bad, "UserID too short should fail")
	assert.not_nil(id_bad_err, "UserID too short should return error")
	assert.error_contains(id_bad_err, "length", "UserID min_len should mention length")

	local acct_ok, acct_err = Account:is({id = "abcd", roles = {"admin"}})
	assert.not_nil(acct_ok, "Account valid should pass")
	assert.is_nil(acct_err, "Account valid should have nil error")

	local acct_bad_roles, acct_bad_roles_err = Account:is({id = "abcd", roles = {}})
	assert.is_nil(acct_bad_roles, "Account with empty roles should fail")
	assert.not_nil(acct_bad_roles_err, "Account with empty roles should return error")
	assert.error_contains(acct_bad_roles_err, "length", "Roles min_len should mention length")

	local acct_bad_role, acct_bad_role_err = Account:is({id = "abcd", roles = {"root"}})
	assert.is_nil(acct_bad_role, "Account with invalid role should fail")
	assert.not_nil(acct_bad_role_err, "Account with invalid role should return error")
	assert.error_contains(acct_bad_role_err, "expected", "Role union should mention expected")

	local acct_bad_note, acct_bad_note_err = Account:is({id = "abcd", roles = {"user"}, note = "x"})
	assert.is_nil(acct_bad_note, "Account note too short should fail")
	assert.not_nil(acct_bad_note_err, "Account note too short should return error")
	assert.error_contains(acct_bad_note_err, "length", "Note min_len should mention length")

	local acct_ok_missing, acct_ok_missing_err = Account:is({id = "abcd", roles = {"user"}})
	assert.not_nil(acct_ok_missing, "Account missing optional note should pass")
	assert.is_nil(acct_ok_missing_err, "Account missing optional note should have nil error")

	return true
end

return { main = main }
