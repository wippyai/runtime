package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"log"
)

// MessageYield represents a yielded message
type MessageYield struct {
	Message lua.LValue
}

func (m *MessageYield) String() string {
	return "message.yield{" + m.Message.String() + "}"
}

func (m *MessageYield) Type() lua.LValueType {
	return lua.LTUserData
}

// ViewYield represents a yielded view result
type ViewYield struct {
	Content lua.LValue
}

func (r *ViewYield) String() string {
	return "view.yield{" + r.Content.String() + "}"
}

func (r *ViewYield) Type() lua.LValueType {
	return lua.LTUserData
}

// IsMessageYield checks if a yielded value is a MessageYield
func IsMessageYield(value lua.LValue) (*MessageYield, bool) {
	if msg, ok := value.(*MessageYield); ok {
		return msg, true
	}
	return nil, false
}

// IsViewYield checks if a yielded value is a ViewYield
func IsViewYield(value lua.LValue) (*ViewYield, bool) {
	if view, ok := value.(*ViewYield); ok {
		return view, true
	}
	return nil, false
}

type BTLayer struct {
	chRun *channel.Runner
	Model *Model
}

func NewBTLayer(chRun *channel.Runner) *BTLayer {
	return &BTLayer{
		chRun: chRun,
		Model: NewModel(),
	}
}

func (b *BTLayer) TryFlushMessage() error {
	if msg, ok := b.Model.TryGetMessage(); ok {
		openChannels := b.chRun.GetOpenChannels()
		for _, ch := range openChannels {
			if ch.Name == "messages" {
				return b.chRun.Send("messages", msg)
			}
		}
	}
	return nil
}

func (b *BTLayer) Step(cvm engine.CVM, tasks ...*engine.Task) ([]*engine.Task, error) {
	outTasks := make([]*engine.Task, 0)
	var err error
	boot := true

	for len(tasks) > 0 || boot {
		boot = false

		// Process current batch of tasks
		tasks, err = cvm.Step(tasks...)
		if err != nil {
			return nil, err
		}

		// Check for message channel and try to flush
		if err := b.TryFlushMessage(); err != nil {
			log.Printf("Error flushing message: %v", err)
		}

		currentTasks := make([]*engine.Task, 0)
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				lastYield := task.Yielded[len(task.Yielded)-1]

				// Handle message yields (ignored for now)
				if _, ok := IsMessageYield(lastYield); ok {
					task.Resumed = []lua.LValue{lua.LTrue}
					currentTasks = append(currentTasks, task)
					continue
				}

				// Handle view yields
				if viewYield, ok := IsViewYield(lastYield); ok {
					b.Model.SendView(viewYield.Content)
					task.Resumed = []lua.LValue{lua.LTrue}
					currentTasks = append(currentTasks, task)
					continue
				}
			}

			outTasks = append(outTasks, task)
		}

		tasks = currentTasks
	}

	return outTasks, nil
}

// GetModel returns the internal Model that implements tea.Model
func (b *BTLayer) GetModel() tea.Model {
	return b.Model
}
