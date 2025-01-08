package main

import (
	"fmt"
	lua "github.com/yuin/gopher-lua"
)

const luaScript = `
function worker()
    print("Starting worker coroutine")
    
    -- Using pcall with yield
    local success, result = pcall(function()
        print("Before yield")
        local value = coroutine.yield("first yield")
        print("After yield, received: " .. value)
        return "completed"
    end)
    
    if success then
        print("Worker succeeded:", result)
    else
        print("Worker failed:", result)
    end
    
    return success, result
end

-- Create the coroutine
co = coroutine.create(worker)
`

func main() {
	L := lua.NewState()
	defer L.Close()

	// Load the Lua script
	if err := L.DoString(luaScript); err != nil {
		fmt.Printf("Failed to load Lua script: %v\n", err)
		return
	}

	// Get the coroutine from global scope
	co := L.GetGlobal("co")
	if co.Type() != lua.LTThread {
		fmt.Println("Failed to get coroutine")
		return
	}

	// First resume to start the coroutine
	if _, err, _ := L.Resume(L, nil, lua.LNil); err != nil {
		fmt.Printf("First resume failed: %v\n", err)
		return
	}

	fmt.Println("Coroutine started successfully")

	// Second resume to continue after yield
	if _, err, _ := L.Resume(L, nil, lua.LString("resumed value")); err != nil {
		fmt.Printf("Second resume failed: %v\n", err)
		return
	}

	fmt.Println("Coroutine completed successfully")
}
