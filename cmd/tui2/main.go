package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Main application model
type model struct {
	tabs       []string
	activeTab  int
	windowSize tea.WindowSizeMsg

	// Tab content models
	dashboardTab dashboardModel
	profileTab   profileModel
	settingsTab  settingsModel
}

// Sub-models for each tab
type dashboardModel struct {
	items []string
}

type profileModel struct {
	name    string
	editing bool
}

type settingsModel struct {
	theme string
}

func initialModel() model {
	return model{
		tabs:      []string{"Dashboard", "Profile", "Settings"},
		activeTab: 0,
		dashboardTab: dashboardModel{
			items: []string{"Item 1", "Item 2"},
		},
		profileTab: profileModel{
			name: "User",
		},
		settingsTab: settingsModel{
			theme: "dark",
		},
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			// Cycle through tabs
			m.activeTab = (m.activeTab + 1) % len(m.tabs)
		case "shift+tab":
			// Cycle backwards
			m.activeTab--
			if m.activeTab < 0 {
				m.activeTab = len(m.tabs) - 1
			}
		default:
			// Handle input based on active tab
			switch m.activeTab {
			case 0:
				return m.updateDashboard(msg)
			case 1:
				return m.updateProfile(msg)
			case 2:
				return m.updateSettings(msg)
			}
		}
	case tea.WindowSizeMsg:
		m.windowSize = msg
	}

	return m, nil
}

// Update functions for each tab
func (m model) updateDashboard(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "a":
			m.dashboardTab.items = append(m.dashboardTab.items, "New Item")
		}
	}
	return m, nil
}

func (m model) updateProfile(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "e":
			m.profileTab.editing = !m.profileTab.editing
		}
	}
	return m, nil
}

func (m model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "t":
			if m.settingsTab.theme == "dark" {
				m.settingsTab.theme = "light"
			} else {
				m.settingsTab.theme = "dark"
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	// Style definitions
	tabStyle := lipgloss.NewStyle().
		Padding(0, 2).
		Bold(true)
	activeTab := tabStyle.Copy().
		Foreground(lipgloss.Color("#FF75B5")).
		Background(lipgloss.Color("#2D2D2D"))
	inactiveTab := tabStyle.Copy().
		Foreground(lipgloss.Color("#888888"))

	// Build tab bar
	var tabBar strings.Builder
	for i, tab := range m.tabs {
		if i == m.activeTab {
			tabBar.WriteString(activeTab.Render(tab))
		} else {
			tabBar.WriteString(inactiveTab.Render(tab))
		}
		tabBar.WriteString(" ")
	}

	// Render active tab content
	var content string
	switch m.activeTab {
	case 0:
		content = m.renderDashboard()
	case 1:
		content = m.renderProfile()
	case 2:
		content = m.renderSettings()
	}

	return fmt.Sprintf("%s\n\n%s\n\nPress 'tab' to switch tabs, 'q' to quit",
		tabBar.String(), content)
}

// View functions for each tab
func (m model) renderDashboard() string {
	var s strings.Builder
	s.WriteString("Dashboard\n\n")
	for _, item := range m.dashboardTab.items {
		s.WriteString(fmt.Sprintf("• %s\n", item))
	}
	s.WriteString("\nPress 'a' to add item")
	return s.String()
}

func (m model) renderProfile() string {
	s := fmt.Sprintf("Profile: %s\n", m.profileTab.name)
	if m.profileTab.editing {
		s += "\nEditing mode (press 'e' to exit)"
	} else {
		s += "\nPress 'e' to edit"
	}
	return s
}

func (m model) renderSettings() string {
	return fmt.Sprintf("Settings\n\nTheme: %s\n\nPress 't' to toggle theme",
		m.settingsTab.theme)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
	}
}
