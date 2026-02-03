local function main(name)
	return "Hello, " .. (name or "Anonymous") .. "!"
end

return { main = main }
