package terminal

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/supervisor"
	"io"
	"strings"
)

type InputField struct {
	label   string
	value   string
	cursor  int
	focused bool
}

type bubbleModel struct {
	inputs   []InputField
	out      io.Writer
	quitting bool
}

func (m bubbleModel) Init() tea.Cmd {
	return tea.EnterAltScreen
}

func (m bubbleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			m.quitting = true
			return m, tea.Quit
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			// Print current input values
			for _, input := range m.inputs {
				fmt.Fprintf(m.out, "%s: %s\n", input.label, input.value)
			}
			// Clear inputs
			for i := range m.inputs {
				m.inputs[i].value = ""
				m.inputs[i].cursor = 0
			}
			return m, nil
		case "tab":
			// Switch focus between input fields
			for i := range m.inputs {
				if m.inputs[i].focused {
					m.inputs[i].focused = false
					m.inputs[(i+1)%len(m.inputs)].focused = true
					break
				}
			}
			return m, nil
		case "backspace":
			for i := range m.inputs {
				if m.inputs[i].focused && m.inputs[i].cursor > 0 {
					m.inputs[i].value = m.inputs[i].value[:m.inputs[i].cursor-1] + m.inputs[i].value[m.inputs[i].cursor:]
					m.inputs[i].cursor--
					break
				}
			}
			return m, nil
		case "left":
			for i := range m.inputs {
				if m.inputs[i].focused && m.inputs[i].cursor > 0 {
					m.inputs[i].cursor--
					break
				}
			}
			return m, nil
		case "right":
			for i := range m.inputs {
				if m.inputs[i].focused && m.inputs[i].cursor < len(m.inputs[i].value) {
					m.inputs[i].cursor++
					break
				}
			}
			return m, nil
		default:
			if msg.Type == tea.KeyRunes {
				for i := range m.inputs {
					if m.inputs[i].focused {
						m.inputs[i].value = m.inputs[i].value[:m.inputs[i].cursor] + string(msg.Runes) + m.inputs[i].value[m.inputs[i].cursor:]
						m.inputs[i].cursor += len(msg.Runes)
						break
					}
				}
			}
		}
	}

	return m, nil
}

func (m bubbleModel) View() string {
	var sb strings.Builder

	// Draw each input field
	for _, input := range m.inputs {
		// Show label
		sb.WriteString(input.label)
		sb.WriteString(": ")

		// Show input value with cursor
		if input.focused {
			if input.cursor == len(input.value) {
				sb.WriteString(input.value + "█")
			} else {
				sb.WriteString(input.value[:input.cursor] + "█" + input.value[input.cursor:])
			}
		} else {
			sb.WriteString(input.value + " ")
		}
		sb.WriteString("\n")
	}

	// Add controls help
	sb.WriteString("\n[Tab] Switch fields • [Enter] Submit • [q] Quit\n")

	return sb.String()
}

type DualInputTerminal struct {
	labels []string
}

func NewDualInputTerminal(labels []string) *DualInputTerminal {
	if len(labels) == 0 {
		labels = []string{"Input 1", "Input 2"}
	}
	return &DualInputTerminal{
		labels: labels,
	}
}

func (t *DualInputTerminal) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	inputs := make([]InputField, len(t.labels))
	for i, label := range t.labels {
		inputs[i] = InputField{
			label:   label,
			focused: i == 0, // Focus first input by default
		}
	}

	model := bubbleModel{
		inputs: inputs,
		out:    out,
	}

	p := tea.NewProgram(
		model,
		tea.WithInput(in),
		tea.WithOutput(out),
		tea.WithAltScreen(),
	)

	go func() { <-ctx.Done(); p.Quit() }()

	m, err := p.Run()
	if err != nil {
		return fmt.Errorf("bubbletea error: %w", err)
	}

	if m.(bubbleModel).quitting {
		return supervisor.Exited
	}

	return nil
}
