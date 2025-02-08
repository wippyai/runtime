package main

import (
	"log"

	lua "github.com/yuin/gopher-lua"
)

// Function to dump stack of any Lua thread
func dumpStack(co *lua.LState) {
	log.Printf("=== Dumping Stack for thread %p ===", co)

	for level := 0; ; level++ {
		if ar, ok := co.GetStack(level); ok {
			funcTable := co.NewTable()
			if _, err := co.GetInfo("Slnf", ar, funcTable); err != nil {
				log.Printf("Error getting info at level %d: %v", level, err)
				continue
			}

			log.Printf("Level %d:", level)
			log.Printf("  Source: %s", ar.Source)
			log.Printf("  Line: %d (defined at %d-%d)",
				ar.CurrentLine, ar.LineDefined, ar.LastLineDefined)
			log.Printf("  Name: %s (%s)", ar.Name, ar.What)

			// Get locals
			for i := 1; ; i++ {
				name, value := co.GetLocal(ar, i)
				if name == "" {
					break
				}
				log.Printf("  Local: %s = %v", name, value)
			}
		} else {
			break
		}
	}
}

// Function that will yield in Lua and let us inspect it
func pauseFunc(L *lua.LState) int {
	// Get the current thread
	thread := L
	log.Printf("Thread in pause func: %p", thread)

	// Dump the stack of the current thread
	dumpStack(thread)

	// Yield instead of returning
	return L.Yield()
}

func main() {
	L := lua.NewState()
	defer L.Close()

	// Register our functions
	L.SetGlobal("pause_func", L.NewFunction(pauseFunc))

	script := `
		function worker()
			local x = 1
			local y = "test"
			pause_func()  -- This will yield and let us inspect
			x = x + 1
			return x
		end

		-- Create coroutine
		local co = coroutine.create(worker)
		
		-- Start it - this will run until pause_func yields
		local status, result = coroutine.resume(co)
		print("Status after pause:", status)
		
		-- Resume to get final result
		status, result = coroutine.resume(co)
		print("Final status:", status)
		print("Final result:", result)
	`

	if err := L.DoString(script); err != nil {
		log.Fatalf("Failed to run script: %v", err)
	}
}
