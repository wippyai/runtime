package terminal

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"io"
	"log"
)

type bubbleModel struct {
	prompt   string
	input    string
	out      io.Writer
	cursor   int
	quitting bool
}

func (m bubbleModel) Init() tea.Cmd {
	return tea.EnterAltScreen
}

func (m bubbleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEnter:
			fmt.Fprintln(m.out, m.input)
			fmt.Fprint(m.out, m.prompt)
			m.input = ""
			m.cursor = 0
			return m, nil
		case tea.KeyBackspace:
			if m.cursor > 0 {
				m.input = m.input[:m.cursor-1] + m.input[m.cursor:]
				m.cursor--
			}
			return m, nil
		case tea.KeyLeft:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case tea.KeyRight:
			if m.cursor < len(m.input) {
				m.cursor++
			}
			return m, nil
		}

		if msg.Type == tea.KeyRunes {
			m.input = m.input[:m.cursor] + string(msg.Runes) + m.input[m.cursor:]
			m.cursor += len(msg.Runes)
		}
	}

	return m, nil
}

func (m bubbleModel) View() string {
	return fmt.Sprintf("%s%s", m.prompt, m.input)
}

type EchoTerminal struct {
	prompt string
}

func NewEchoTerminal(prompt string) *EchoTerminal {
	if prompt == "" {
		prompt = "> "
	}
	return &EchoTerminal{
		prompt: prompt,
	}
}

func (t *EchoTerminal) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	model := bubbleModel{
		prompt: t.prompt,
		out:    out,
	}

	p := tea.NewProgram(model, tea.WithAltScreen())

	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	log.Printf("STEAT")
	m, err := p.Run()
	if err != nil {
		return fmt.Errorf("bubbletea error: %w", err)
	}

	if m.(bubbleModel).quitting {
		return context.Canceled
	}

	return nil
}
