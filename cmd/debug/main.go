package main

import (
	"log"

	lua "github.com/yuin/gopher-lua"
)

// Function that will inspect its own stack
func inspectStack(L *lua.LState) int {
	log.Println("=== Getting Stack Information ===")

	// Try different stack levels
	for level := 0; level < 3; level++ {
		log.Printf("\nInspecting stack level: %d", level)

		if ar, ok := L.GetStack(level); ok {
			// Create empty LTable for function
			funcTable := L.NewTable()

			// Get detailed info about this stack frame
			// 'S': source info
			// 'l': current line
			// 'n': name info
			// 'f': function itself
			if _, err := L.GetInfo("Slnf", ar, funcTable); err != nil {
				log.Printf("Error getting info: %v", err)
				continue
			}

			log.Printf("Source: %s", ar.Source)
			log.Printf("Current line: %d", ar.CurrentLine)
			log.Printf("Line defined: %d", ar.LineDefined)
			log.Printf("Last line defined: %d", ar.LastLineDefined)
			log.Printf("What: %s", ar.What)
			log.Printf("Name: %s", ar.Name)

			// Get local variables at this level
			for i := 1; ; i++ {
				name, value := L.GetLocal(ar, i)
				if name == "" {
					break
				}
				log.Printf("Local %d: %s = %v", i, name, value)
			}

			// Get function from the table
			if fn := funcTable.RawGet(lua.LString("f")); fn != lua.LNil {
				luaFn, ok := fn.(*lua.LFunction)
				if ok {
					log.Printf("Found function at level %d", level)
					// Now we can get upvalues
					for i := 1; ; i++ {
						name, val := L.GetUpvalue(luaFn, i)
						if name == "" {
							break
						}
						log.Printf("Upvalue %d: %s = %v", i, name, val)
					}
				}
			}
		} else {
			log.Printf("No stack information available at level %d", level)
			break
		}
	}

	return 0
}

func main() {
	L := lua.NewState()
	defer L.Close()

	// Register our inspection function
	L.SetGlobal("inspect_stack", L.NewFunction(inspectStack))

	// Create a test script that will give us interesting stack frames
	script := `
		-- Global variable
		global_var = "I'm global"

		-- Function with upvalue
		local function make_counter()
			local count = 0  -- This will be an upvalue
			return function()
				count = count + 1
				return count
			end
		end

		-- Create a counter
		local counter = make_counter()

		-- Function that will create a stack
		function deep_function(x)
			local local_var = "I'm local"
			-- Call our inspection function
			inspect_stack()
			return x + 1
		end

		-- Function to create more stack frames
		function outer_function(y)
			local another_var = "outer local"
			return deep_function(y)
		end

		-- Let's call it
		result = outer_function(5)
		print("Result:", result)

		-- Test the counter
		print("Counter:", counter())
		print("Counter:", counter())
	`

	if err := L.DoString(script); err != nil {
		log.Fatalf("Failed to run script: %v", err)
	}
}
