-- SPDX-License-Identifier: MPL-2.0

type Config = {host: string, port: number}
type Host = string @min_len(1) @max_len(64)
type Port = number @min(1) @max(65535)
type Tags = {string} @min_len(1)
type Server = {host: Host, port: Port, tags?: Tags}
type Email = string @pattern("^.+@.+\\..+$")
type User = {id: string, email: Email, roles: Tags}

local function create(host: string, port: number): Config
	return {host = host, port = port}
end

local function create_server(host: string, port: number, tags: {string}?): Server
	return ({host = host, port = port, tags = tags} as Server)
end

local function create_user(id: string, email: string, roles: {string}): User
	return ({id = id, email = email, roles = roles} as User)
end

return {
	create = create,
	create_server = create_server,
	create_user = create_user,
	Config = Config,
	Host = Host,
	Port = Port,
	Tags = Tags,
	Server = Server,
	Email = Email,
	User = User
}
