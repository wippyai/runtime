package btea

import (
	"context"
	"fmt"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/service/terminal"
	"sync"
)

type keyMap struct {
	Quit key.Binding
}

var keys = keyMap{
	Quit: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "quit"),
	),
}

// Model represents the terminal UI state
type Model struct {
	width    int
	height   int
	content  string
	error    error
	quitting bool
	ctx      context.Context
	terminal *terminal.PipeContext
	program  *tea.Program
	keys     keyMap
}

// App represents a terminal application process with Bubble Tea
type App struct {
	pid      process.PID
	ctx      context.Context
	model    *Model
	program  *tea.Program
	mu       sync.Mutex
	done     chan struct{}
	terminal *terminal.PipeContext
}

// NewTerminalProcess creates a new terminal process instance
func NewTerminalProcess() process.Process {
	return &App{
		done: make(chan struct{}),
	}
}

// Start initializes the terminal process with Bubble Tea
func (p *App) Start(ctx context.Context, pid process.PID, input payload.Payloads) error {
	p.ctx = ctx
	p.pid = pid

	term := terminal.FromContext(ctx)
	if term == nil {
		return fmt.Errorf("terminal context not found")
	}
	p.terminal = term

	model := &Model{
		ctx:      ctx,
		terminal: term,
		keys:     keys,
	}
	p.model = model

	program := tea.NewProgram(
		model,
		tea.WithInput(term.Stdin),
		tea.WithOutput(term.Stdout),
		tea.WithAltScreen(),
	)
	p.program = program
	model.program = program

	go func() {
		if _, err := program.Run(); err != nil {
			p.model.error = err
		}
		close(p.done)
	}()

	if onStart := process.GetOnStart(ctx); onStart != nil {
		onStart(pid, p)
	}

	return nil
}

// Step handles the terminal process state updates
func (p *App) Step() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	select {
	case <-p.done:
		if p.model.error != nil {
			return p.model.error
		}
		if p.model.quitting {
			if onComplete := process.GetOnComplete(p.ctx); onComplete != nil {
				onComplete(p.pid, &runtime.Result{Payload: payload.NewString("quit")})
			}
		}
		return nil
	default:
	}

	return nil
}

// Send handles incoming messages
func (p *App) Send(msg ...*process.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, m := range msg {
		if m.Topic == process.TopicCancel {
			p.program.Quit()
			return nil
		}
	}

	return nil
}

// Init initializes the Bubble Tea model
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update handles events and updates the model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if key.Matches(msg, m.keys.Quit) {
			m.quitting = true
			return m, tea.Quit
		}
	}

	return m, nil
}

// View renders the current state
func (m *Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	return fmt.Sprintf(
		"Window size: %d x %d\n\nPress 'q', 'esc', or 'ctrl+c' to quit",
		m.width,
		m.height,
	)
}
