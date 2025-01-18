package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	lua "github.com/yuin/gopher-lua"
	"strings"
)

// Model represents the internal state and implements tea.Model
type Model struct {
	// Channel-related fields
	msgs  chan tea.Msg
	views chan string
}

func NewModel() *Model {
	return &Model{
		msgs:  make(chan tea.Msg),
		views: make(chan string),
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.EnterAltScreen
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.msgs <- msg
	return m, nil

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowSize = msg
		m.windowEvents = append(m.windowEvents, fmt.Sprintf("Window resized to %dx%d", msg.Width, msg.Height))

	case tea.MouseMsg:
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
	}

	// Keep only last 5 events
	if len(m.windowEvents) > 5 {
		m.windowEvents = m.windowEvents[len(m.windowEvents)-5:]
	}

	return m, nil
}

func (m *Model) View() string {
	style := lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(lipgloss.Color("#FF75B5"))

	var b strings.Builder
	b.WriteString("Pony Runtime Interface\n\n")

	b.WriteString(style.Render(fmt.Sprintf("Window Size: %dx%d\n", m.windowSize.Width, m.windowSize.Height)))
	b.WriteString(style.Render(fmt.Sprintf("Active: %v\n", m.active)))
	b.WriteString(style.Render(fmt.Sprintf("Last Key: %s\n", m.lastKey)))

	b.WriteString("\nRecent Events:\n")
	for _, event := range m.windowEvents {
		b.WriteString(style.Render("• " + event + "\n"))
	}

	return b.String()
}

// Channel operation methods
func (m *Model) QueueMessage(msg lua.LValue) {
	select {
	case m.msgs <- msg:
		m.windowEvents = append(m.windowEvents, "Message queued")
	default:
		m.windowEvents = append(m.windowEvents, "Message queue full")
	}
}

func (m *Model) TryGetMessage() (lua.LValue, bool) {
	select {
	case msg := <-m.msgs:
		return msg, true
	default:
		return nil, false
	}
}

func (m *Model) SendView(view lua.LValue) {
	select {
	case m.views <- view:
		m.windowEvents = append(m.windowEvents, "View update sent")
	default:
		m.windowEvents = append(m.windowEvents, "View channel blocked")
	}
}

func (m *Model) GetNextView() lua.LValue {
	return <-m.views
}
