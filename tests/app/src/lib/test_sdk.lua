-- Test SDK library for eval imports testing
local sdk = {}

sdk.version = "2.0.0"
sdk.name = "TestSDK"

function sdk.greet(name)
    return "Hello, " .. (name or "World") .. "!"
end

function sdk.add(a, b)
    return a + b
end

function sdk.create_object(name, value)
    return {
        name = name,
        value = value,
        get_info = function(self)
            return self.name .. ": " .. tostring(self.value)
        end
    }
end

return sdk
