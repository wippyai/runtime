package engine2

import lua "github.com/yuin/gopher-lua"

// BindCpcall binds the cpcall function for protected coroutine calls.
// cpcall runs a function in an isolated coroutine and catches errors.
// Returns (true, results...) on success or (false, error) on failure.
func BindCpcall(l *lua.LState, proc *Process) {
	l.SetGlobal("cpcall", l.NewFunction(func(l *lua.LState) int {
		fn := l.CheckFunction(1)

		// Collect arguments
		nargs := l.GetTop() - 1
		args := make([]lua.LValue, nargs)
		for i := 0; i < nargs; i++ {
			args[i] = l.Get(i + 2)
		}

		// Get caller task and mark as blocked
		callerTask, err := proc.GetTask(l)
		if err != nil {
			l.RaiseError("cpcall: cannot find caller task")
			return 0
		}
		callerTask.SetBlocked(true)

		// Create isolated task
		newTask := proc.createTask(fn)
		newTask.Resumed = args
		newTask.SetPcallFrom(l)

		// Yield to let scheduler run the new task
		return -1
	}))
}

// HandleCpcallCompletion checks if a completed task was from cpcall and resumes caller.
// Should be called when a task completes (either normally or with error).
func HandleCpcallCompletion(proc *Process, task *Task, taskErr error) {
	pcallFrom := task.PcallFrom()
	if pcallFrom == nil {
		return
	}

	// Find the caller task
	callerTask, err := proc.GetTask(pcallFrom)
	if err != nil {
		return
	}

	// Unblock caller
	callerTask.SetBlocked(false)

	// Prepare results
	if taskErr != nil {
		// Error case: return false, error message
		callerTask.Resumed = []lua.LValue{lua.LFalse, lua.LString(taskErr.Error())}
	} else {
		// Success case: return true, results...
		results := make([]lua.LValue, 1+len(task.Yielded))
		results[0] = lua.LTrue
		copy(results[1:], task.Yielded)
		callerTask.Resumed = results
	}

	// Re-queue caller
	proc.queue.Push(callerTask)
}
