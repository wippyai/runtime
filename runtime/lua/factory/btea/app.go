package btea

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/service/terminal"
	"sync"
)

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
}

// TerminalProcess represents a terminal application process with Bubble Tea
type TerminalProcess struct {
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
	return &TerminalProcess{
		done: make(chan struct{}),
	}
}

// Start initializes the terminal process with Bubble Tea
func (p *TerminalProcess) Start(ctx context.Context, pid process.PID, input payload.Payloads) error {
	p.ctx = ctx
	p.pid = pid

	// Get terminal context
	term := terminal.FromContext(ctx)
	if term == nil {
		return fmt.Errorf("terminal context not found")
	}
	p.terminal = term

	// Initialize model
	model := &Model{
		ctx:      ctx,
		terminal: term,
	}
	p.model = model

	// Create and start the Bubble Tea program
	program := tea.NewProgram(
		model,
		tea.WithInput(term.Stdin),
		tea.WithOutput(term.Stdout),
		tea.WithAltScreen(),
	)
	p.program = program
	model.program = program

	// Start the program in a goroutine
	go func() {
		if _, err := program.Run(); err != nil {
			p.model.error = err
		}
		close(p.done)
	}()

	// Notify process started
	if onStart := process.GetOnStart(ctx); onStart != nil {
		onStart(pid, p)
	}

	return nil
}

// Step handles the terminal process state updates
func (p *TerminalProcess) Step() error {
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
func (p *TerminalProcess) Send(msg ...*process.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, m := range msg {
		if m.Topic == process.TopicCancel {
			p.program.Quit()
			return nil
		}
		// Handle other messages here
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
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	}
	return m, nil
}

// View renders the current state
func (m *Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	// Create a simple view with window dimensions
	return fmt.Sprintf(
		"Window size: %d x %d\n\nPress 'q' to quit",
		m.width,
		m.height,
	)
}
