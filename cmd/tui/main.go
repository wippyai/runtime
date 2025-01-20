package main

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"go.uber.org/zap"
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

func main() {
	log := zap.NewNop()
	vm, err := engine.NewCVM(log, engine.WithPreloaded("channel", channel.NewChannelModule().Loader))
	if err != nil {
		fmt.Printf("Error creating VM: %v", err)
		return
	}

	vm.Import(`
function app()
	print("Hello from app")
end
`, "app_code", "app")

	wvm := engine.NewRunner(vm,
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		// todo: add model layer!
	)
	defer wvm.Close()

	go func() {
		_, err := wvm.Execute(context.Background(), "app")
		if err != nil {
			fmt.Printf("Error executing VM: %v", err)
			return
		}
	}()

	p := tea.NewProgram(model{},
		tea.WithAltScreen(),       // Use alternate screen buffer
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
	}

	// wait for exit
	select {}
}
