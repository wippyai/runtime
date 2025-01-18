package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	httpmod "github.com/ponyruntime/pony/runtime/lua/modules/http"
	"github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/ponyruntime/pony/runtime/lua/modules/time"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func main() {
	log := zap.NewNop()
	vm, err := engine.NewCVM(
		log,
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("http", httpmod.NewHTTPModule(http.DefaultClient, log).Loader),
		engine.WithPreloaded("json", json.NewJsonModule().Loader),
		engine.WithPreloaded("time", time.NewTimeModule().Loader),
		engine.WithGlobalFunction("yield_message", func(L *lua.LState) int {
			msg := L.CheckAny(1)
			L.Push(&MessageYield{Message: msg})
			return -1
		}),
		engine.WithGlobalFunction("yield_view", func(L *lua.LState) int {
			content := L.CheckAny(1)
			L.Push(&ViewYield{Content: content})
			return -1
		}),
	)
	if err != nil {
		fmt.Printf("Error creating VM: %v", err)
		return
	}

	code, err := os.ReadFile("app2.lua")
	if err != nil {
		fmt.Printf("Error reading app code: %v", err)
		return
	}

	err = vm.Import(string(code), "app_code", "App")
	if err != nil {
		fmt.Printf("Error importing code: %v", err)
		return
	}

	chRun := channel.NewChannelRunner()
	btLayer := &BTLayer{chRun: chRun}

	wvm := engine.NewWrappedCVM(vm,
		engine.WithLayer(chRun),
		engine.WithLayer(btLayer),
		engine.WithLayer(coroutine.NewCoroutineRunner()),
	)
	defer wvm.Close()

	_, err = wvm.Execute(context.Background(), "App")
	if err != nil {
		fmt.Printf("Error executing VM: %+v", err)
		return
	}

	select {}
}
