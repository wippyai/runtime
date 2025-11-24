package main

import (
	"context"
	"fmt"
	"os"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	sandbox "github.com/wippyai/runtime/runtime/lua/modules/workflow_sandbox"
	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, logger)

	bus := &mockEventBus{}
	ctx = event.WithBus(ctx, bus)

	cm, err := code.NewCodeManager(logger, bus, code.Config{
		Modules:        []luaapi.Module{},
		ProtoCacheSize: 1000,
		MainCacheSize:  100,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create code manager: %v\n", err)
		os.Exit(1)
	}

	ctx = luaapi.SetCodeManager(ctx, cm)

	err = registerWorkflow(ctx, cm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to register workflow: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Running workflow sandbox test...")

	result, err := runTestTool(ctx, cm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "test failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Test completed successfully: %v\n", result)
}

func registerWorkflow(ctx context.Context, cm *code.Manager) error {
	id := registry.ParseID("app.workflows:simple_workflow")

	source := `
function main(input)
    local timer_req = upstream.request("timer.sleep", 5000)
    timer_req:response():receive()
    return "timer completed: " .. input
end
`

	node := code.Node{
		ID:     id,
		Kind:   luaapi.KindWorkflow,
		Source: source,
		Method: "main",
	}

	return cm.AddNode(ctx, node, []code.Import{})
}

type mockEventBus struct{}

func (b *mockEventBus) Send(_ context.Context, _ event.Event) {}
func (b *mockEventBus) Subscribe(_ context.Context, _ event.System, _ chan<- event.Event) (event.SubscriberID, error) {
	return "mock", nil
}
func (b *mockEventBus) SubscribeP(_ context.Context, _ event.System, _ event.Kind, _ chan<- event.Event) (event.SubscriberID, error) {
	return "mock", nil
}
func (b *mockEventBus) Unsubscribe(_ context.Context, _ event.SubscriberID) {}

func runTestTool(ctx context.Context, cm *code.Manager) (interface{}, error) {
	source := `
local workflow_sandbox = require("workflow_sandbox")

function main()
    local wf, err = workflow_sandbox.get("app.workflows:simple_workflow")
    if err then
        error("failed to get workflow: " .. tostring(err))
    end

    print("Starting workflow...")

    local ok, err = wf:step("main", "test input")
    if not ok then
        error("step failed: " .. tostring(err))
    end

    local commands = wf:commands()
    print("Got " .. #commands .. " commands")

    for i, cmd in ipairs(commands) do
        print("Command " .. i .. ": " .. cmd:type())
        cmd:complete(true)
    end

    local ok, err = wf:step()
    if not ok then
        error("step 2 failed: " .. tostring(err))
    end

    if wf:done() then
        print("Workflow completed!")
        local result, err = wf:result()
        if err then
            error("error getting result: " .. tostring(err))
        end
        print("Result: " .. tostring(result))
    else
        print("Workflow not done yet")
    end

    wf:close()

    return "test completed"
end
`

	return evalSource(ctx, cm, source, "main")
}

func evalSource(ctx context.Context, cm *code.Manager, source, method string) (interface{}, error) {
	runner, err := buildRunner(ctx, cm, source, method)
	if err != nil {
		return nil, fmt.Errorf("failed to build runner: %w", err)
	}
	defer runner.Close()

	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	result, err := runner.Execute(frameCtx, method)
	if err != nil {
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	return result, nil
}

func buildRunner(ctx context.Context, cm *code.Manager, source, method string) (*engine.Runner, error) {
	mod := sandbox.NewWorkflowSandboxModule()

	cvm, err := engine.NewCVM(nil, engine.WithLoader(mod.Name(), mod.Loader))
	if err != nil {
		return nil, fmt.Errorf("failed to create CVM: %w", err)
	}

	if err := cvm.Import(source, "test", method); err != nil {
		cvm.Close()
		return nil, fmt.Errorf("failed to import source: %w", err)
	}

	runner := engine.NewRunner(cvm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	return runner, nil
}
