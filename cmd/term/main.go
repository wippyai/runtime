package main

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	httpmod "github.com/ponyruntime/pony/runtime/lua/modules/http"
	"github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/ponyruntime/pony/runtime/lua/modules/time"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"log"
	"net/http"
	"os"
	"strings"
)

type model struct {
	windowSize   tea.WindowSizeMsg
	mousePos     tea.MouseMsg
	active       bool
	lastKey      string
	windowEvents []string
	vm           engine.VM
}

func (m model) Init() tea.Cmd {
	// Enable alternate screen
	return tea.EnterAltScreen
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	// System Messages
	case tea.WindowSizeMsg:
		// Terminal window was resized
		m.windowSize = msg
		m.windowEvents = append(m.windowEvents, fmt.Sprintf("Window resized to %dx%d", msg.Width, msg.Height))

	case tea.MouseMsg:
		// Mouse event occurred
		m.mousePos = msg
		m.windowEvents = append(m.windowEvents, fmt.Sprintf("Mouse %s at %d,%d", msg.Type, msg.X, msg.Y))

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEscape:
			return m, tea.Quit
		case tea.KeyTab:
			m.active = !m.active
		default:
			m.lastKey = msg.String()
		}

	// Custom system messages
	case errMsg:
		m.windowEvents = append(m.windowEvents, "Error: "+string(msg))
	}

	// Keep only last 5 events
	if len(m.windowEvents) > 5 {
		m.windowEvents = m.windowEvents[len(m.windowEvents)-5:]
	}

	return m, nil
}

func (m model) View() string {
	style := lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(lipgloss.Color("#FF75B5"))

	var b strings.Builder
	b.WriteString("Terminal Events Demo\n\n")

	// Window info
	b.WriteString(style.Render(fmt.Sprintf("Window Size: %dx%d\n", m.windowSize.Width, m.windowSize.Height)))
	b.WriteString(style.Render(fmt.Sprintf("Mouse Position: %d,%d\n", m.mousePos.X, m.mousePos.Y)))
	b.WriteString(style.Render(fmt.Sprintf("Active: %v\n", m.active)))
	b.WriteString(style.Render(fmt.Sprintf("Last Key: %s\n", m.lastKey)))

	// Recent events
	b.WriteString("\nRecent Events:\n")
	for _, event := range m.windowEvents {
		b.WriteString(style.Render("• " + event + "\n"))
	}

	b.WriteString("\nControls:\n")
	b.WriteString("• Tab: Toggle active state\n")
	b.WriteString("• Esc/Ctrl+C: Quit\n")

	return b.String()
}

// MessageYield represents a yielded message that should be handled by BTLayer
type MessageYield struct {
	Message lua.LValue
}

func (m *MessageYield) String() string {
	return "message.yield{" + m.Message.String() + "}"
}

func (m *MessageYield) Type() lua.LValueType {
	return lua.LTUserData
}

// IsMessageYield checks if a yielded value is a MessageYield
func IsMessageYield(value lua.LValue) (*MessageYield, bool) {
	if msg, ok := value.(*MessageYield); ok {
		return msg, true
	}

	return nil, false
}

// Custom error message type
type errMsg string

type BTLayer struct {
	chRun *channel.Runner
}

func (b *BTLayer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	outTasks := make([]*engine.Task, 0)
	var err error
	boot := true

	for len(tasks) > 0 || boot {
		boot = false

		// Handle channel operations
		openCh := b.chRun.GetActiveChannels()
		if len(openCh) > 0 {
			//for _, ch := range openCh {
			//	//err := b.chRun.Send(ch.Name, lua.LString("Hello from BTLayer"))
			//	//if err != nil {
			//	//					return nil, err
			//	//				}
			//}
		}

		// Process current batch of tasks
		tasks, err = cvm.Step(tasks...)
		if err != nil {
			return nil, err
		}

		currentTasks := make([]*engine.Task, 0)
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				lastYield := task.Yielded[len(task.Yielded)-1]

				// Check if this is a message yield
				if msgYield, ok := IsMessageYield(lastYield); ok {
					// Handle the message
					log.Printf("Handling message yield: %v", msgYield.Message)

					// Resume the task with result
					task.Resumed = []lua.LValue{lua.LTrue}
					currentTasks = append(currentTasks, task)
					continue
				}
			}
			// Pass through non-message yields
			outTasks = append(outTasks, task)
		}

		// Set up next iteration
		tasks = currentTasks
	}

	return outTasks, nil
}

func main() {
	log := zap.NewNop()
	vm, err := engine.NewCVM(
		log,
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("http", httpmod.NewHTTPModule(http.DefaultClient, log).Loader),
		engine.WithPreloaded("json", json.NewJsonModule().Loader),
		engine.WithPreloaded("time", time.NewTimeModule().Loader),
		engine.WithGlobalFunction("send_message", func(L *lua.LState) int {
			msg := L.CheckAny(1)
			L.Push(&MessageYield{Message: msg})
			return -1
		}),
	)
	if err != nil {
		fmt.Printf("Error creating VM: %v", err)
		return
	}

	// Load the app code
	code, err := os.ReadFile("app.lua")

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
		engine.WithLayer(async.NewAsyncRunner(chRun)),
		engine.WithLayer(coroutine.NewCoroutineRunner()),
	)
	defer wvm.Close()

	ctx := context.Background()
	ctx = engine.WithTaskGroup(ctx, engine.NewTaskGroup(1024))
	ctx = async.WithAsyncChannel(ctx)

	_, err = wvm.Execute(ctx, "App")
	if err != nil {
		fmt.Printf("Error executing VM: %v", err)
		return
	}

	// wait for exit
	select {}

}
