package process

import (
	"context"
	"testing"

	apic "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessBasic(t *testing.T) {
	t.Skip("not ready yet")
	// Setup logger and context
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	ctx = context.WithValue(ctx, apic.LoggerCtx, logger)

	// Create process module
	mod := NewModule()
	vm, err := engine.NewVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Run test
	err = vm.DoString(ctx, `
		local process = require("process")
		-- Create a new process
		local proc = process.new("echo 'hello world'")
		assert(proc ~= nil, "Process creation should succeed")
		proc:start()

		-- Read from the process with streaming
		local stream, err = proc:read() -- 4KB buffer
		if err then
    		error("Failed to create read stream: " .. err)
      	end

       	-- Read chunks using iterator
        for chunk in stream do
        	if chunk then
         		print("Received:", chunk)
           	else
            	break
            end
        end
	`, "test")

	assert.NoError(t, err)
}

// todO: fix
