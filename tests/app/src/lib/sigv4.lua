-- SPDX-License-Identifier: MPL-2.0

-- AWS Signature Version 4 signer, pure Lua on top of the hash module.
--
-- The signing-key chain requires each HMAC's raw output to become the next
-- key, so it relies on hash.hmac_sha256(data, key, true) returning raw bytes.
-- hash.sha256 / hash.hmac_sha256 default to lowercase hex, which is what the
-- canonical-request and final-signature steps need.
local hash = require("hash")

local ALGORITHM = "AWS4-HMAC-SHA256"

-- SignRequest describes a request to sign. headers must include host and
-- x-amz-date; every header present is part of the signature.
type SignRequest = {
	method: string,
	uri?: string,
	query?: string,
	headers: {[string]: string},
	body?: string,
	content_sha256?: string,
	access_key: string,
	secret_key: string,
	region: string,
	service: string,
	amz_date: string
}

type SignResult = {
	canonical_request: string,
	string_to_sign: string,
	signature: string,
	authorization: string,
	signed_headers: string,
	payload_hash: string
}

-- hash.* return (value, error?); narrow to a definite string so the raw
-- signing-key output can be chained as the next HMAC key under strict types.
local function hmac_raw(data: string, key: string): string
	local out, err = hash.hmac_sha256(data, key, true)
	if err then
		error(err)
	end
	return out or ""
end

local function hmac_hex(data: string, key: string): string
	local out, err = hash.hmac_sha256(data, key)
	if err then
		error(err)
	end
	return out or ""
end

local function sha256_hex(data: string): string
	local out, err = hash.sha256(data)
	if err then
		error(err)
	end
	return out or ""
end

local function trim(s: string): string
	return (s:gsub("^%s+", ""):gsub("%s+$", ""))
end

-- derive_signing_key returns the raw SigV4 signing key for the credential scope.
local function derive_signing_key(secret: string, date_stamp: string, region: string, service: string): string
	local k_date: string = hmac_raw(date_stamp, "AWS4" .. secret)
	local k_region: string = hmac_raw(region, k_date)
	local k_service: string = hmac_raw(service, k_region)
	return hmac_raw("aws4_request", k_service)
end

-- sign computes the canonical request, string to sign, signature, and the
-- Authorization header value for req.
local function sign(req: SignRequest): SignResult
	local headers = req.headers
	local uri: string = req.uri or "/"
	local query: string = req.query or ""
	local body: string = req.body or ""
	local amz_date: string = req.amz_date
	local date_stamp: string = amz_date:sub(1, 8)

	local names: {string} = {}
	local values: {[string]: string} = {}
	for name, value in pairs(headers) do
		local lname: string = name:lower()
		values[lname] = trim(value)
		names[#names + 1] = lname
	end
	table.sort(names)

	local canonical_headers: string = ""
	for _, lname in ipairs(names) do
		local value: string = values[lname] or ""
		canonical_headers = canonical_headers .. lname .. ":" .. value .. "\n"
	end
	local signed_headers: string = table.concat(names, ";")

	local payload_hash: string = req.content_sha256 or sha256_hex(body)

	local canonical_request: string = req.method .. "\n"
		.. uri .. "\n"
		.. query .. "\n"
		.. canonical_headers .. "\n"
		.. signed_headers .. "\n"
		.. payload_hash

	local scope: string = date_stamp .. "/" .. req.region .. "/" .. req.service .. "/aws4_request"
	local string_to_sign: string = ALGORITHM .. "\n"
		.. amz_date .. "\n"
		.. scope .. "\n"
		.. sha256_hex(canonical_request)

	local signing_key: string = derive_signing_key(req.secret_key, date_stamp, req.region, req.service)
	local signature: string = hmac_hex(string_to_sign, signing_key)

	local authorization: string = ALGORITHM
		.. " Credential=" .. req.access_key .. "/" .. scope
		.. ", SignedHeaders=" .. signed_headers
		.. ", Signature=" .. signature

	return ({
		canonical_request = canonical_request,
		string_to_sign = string_to_sign,
		signature = signature,
		authorization = authorization,
		signed_headers = signed_headers,
		payload_hash = payload_hash
	} as SignResult)
end

return {
	sign = sign,
	derive_signing_key = derive_signing_key,
	SignRequest = SignRequest,
	SignResult = SignResult
}
