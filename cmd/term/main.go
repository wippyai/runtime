package main

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	httpmod "github.com/ponyruntime/pony/runtime/lua/modules/http"
	"github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/ponyruntime/pony/runtime/lua/modules/time"
	"go.uber.org/zap"
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

// Custom error message type
type errMsg string

type BTLayer struct {
	chRun *channel.Runner
}

func (b *BTLayer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	//log.Printf("%v", b.chRun.GetOpenChannels())

	//log.Printf("BTLayer Step")
	return cvm.Step(tasks...)
}

func main() {
	log := zap.NewNop()
	vm, err := engine.NewCVM(
		log,
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("http", httpmod.NewHTTPModule(http.DefaultClient, log).Loader),
		engine.WithPreloaded("json", json.NewJsonModule().Loader),
		engine.WithPreloaded("time", time.NewTimeModule().Loader),
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
		engine.WithLayer(coroutine.NewCoroutineRunner()),
	)
	defer wvm.Close()

	_, err = wvm.Execute(context.Background(), "App")
	if err != nil {
		fmt.Printf("Error executing VM: %v", err)
		return
	}

	// wait for exit
	select {}

}
