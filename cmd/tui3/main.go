package main

import (
	"fmt"
	"math/rand"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Custom message types
type fetchDataMsg string
type timerTickMsg time.Time
type randomNumberMsg int

type model struct {
	status   string
	number   int
	loading  bool
	lastTick time.Time
}

// Custom command that simulates fetching data
func fetchData() tea.Cmd {
	return func() tea.Msg {
		// Simulate API call
		time.Sleep(2 * time.Second)
		return fetchDataMsg("Data fetched successfully!")
	}
}

// Custom command that returns a random number after delay
func generateRandomNumber() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(1 * time.Second)
		return randomNumberMsg(rand.Intn(100))
	}
}

// Custom periodic tick command
func tickEvery() tea.Cmd {
	return tea.Every(time.Second, func(t time.Time) tea.Msg {
		return timerTickMsg(t)
	})
}

func initialModel() model {
	return model{
		status:  "Ready",
		loading: false,
	}
}

func (m model) Init() tea.Cmd {
	// Start with multiple commands using tea.Batch
	return tea.Batch(
		tickEvery(),
		generateRandomNumber(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "f":
			m.loading = true
			m.status = "Fetching data..."
			return m, fetchData()
		case "r":
			m.status = "Generating random number..."
			return m, generateRandomNumber()
		}

	// Handle our custom messages
	case fetchDataMsg:
		m.loading = false
		m.status = string(msg)
		return m, nil

	case randomNumberMsg:
		m.number = int(msg)
		m.status = "Generated new random number"
		return m, nil

	case timerTickMsg:
		m.lastTick = time.Time(msg)
		return m, nil
	}

	return m, nil
}

func (m model) View() string {
	style := lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(lipgloss.Color("#FF75B5"))

	var s string
	s += style.Render("Custom Commands Demo\n\n")
	s += style.Render(fmt.Sprintf("Status: %s\n", m.status))
	s += style.Render(fmt.Sprintf("Random Number: %d\n", m.number))
	s += style.Render(fmt.Sprintf("Last Tick: %s\n", m.lastTick.Format("15:04:05")))

	if m.loading {
		s += style.Render("\nLoading...\n")
	}

	s += "\nCommands:\n"
	s += "f: Fetch data\n"
	s += "r: Generate random number\n"
	s += "q: Quit\n"

	return s
}

func main() {
	rand.Seed(time.Now().UnixNano())
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
	}
}
