package main

import (
	"fmt"
	"log"

	"github.com/yuin/gopher-lua"
)

type Task struct {
	l           *lua.LState
	thread      *lua.LState
	state       lua.ResumeState
	yieldedVals []lua.LValue
	resumeVal   lua.LValue
}

func (t *Task) IsYielded() bool {
	return t.state == lua.ResumeYield
}

func (t *Task) GetYieldedValues() []lua.LValue {
	return t.yieldedVals
}

func (t *Task) SetResumeValue(val lua.LValue) {
	t.resumeVal = val
}

type Scheduler struct {
	L     *lua.LState
	tasks []*Task
}

func NewScheduler(L *lua.LState) *Scheduler {
	return &Scheduler{
		L:     L,
		tasks: []*Task{},
	}
}

func (s *Scheduler) AddTask(fn *lua.LFunction) *Task {
	co, _ := s.L.NewThread()
	task := &Task{
		l:      s.L,
		thread: co,
	}

	state, err, values := s.L.Resume(co, fn)
	if err != nil {
		log.Printf("[createCoroutine] Error starting coroutine: %v\n", err)
		return nil
	}

	task.state = state
	task.yieldedVals = values

	s.tasks = append(s.tasks, task)
	return task
}

func (s *Scheduler) Step(tasks ...*Task) ([]*Task, error) {
	newlyYielded := make([]*Task, 0)

	for _, task := range tasks {
		if task.state != lua.ResumeYield {
			continue
		}

		state, err, values := s.L.Resume(task.thread, nil, task.resumeVal)
		if err != nil {
			return nil, fmt.Errorf("error resuming task: %v", err)
		}

		task.state = state
		task.yieldedVals = values
		task.resumeVal = nil

		if state == lua.ResumeYield {
			newlyYielded = append(newlyYielded, task)
		}
	}

	return newlyYielded, nil
}

func (s *Scheduler) GetYieldedTasks() []*Task {
	yielded := make([]*Task, 0)
	for _, task := range s.tasks {
		if task.state == lua.ResumeYield {
			yielded = append(yielded, task)
		}
	}
	return yielded
}

func (s *Scheduler) RemoveTask(task *Task) {
	for i, t := range s.tasks {
		if t == task {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			return
		}
	}
}

func main() {
	L := lua.NewState()
	defer L.Close()

	scheduler := NewScheduler(L)

	L.SetGlobal("scheduler", L.NewTable())
	L.SetField(L.GetGlobal("scheduler"), "createCoroutine", L.NewFunction(func(L *lua.LState) int {
		fn := L.CheckFunction(1)
		scheduler.AddTask(fn)
		return 0
	}))

	if err := L.DoString(`
		function create_request(type, ...)
			return {type = type, params = {...}}
		end

		function exec_async(...)
			print("[Lua] Creating batch request")
			local requests = {...}
			print("[Lua] Yielding batch with " .. #requests .. " requests")
			local results = coroutine.yield("batch", requests)
			print("[Lua] Received batch results")
			return results
		end

		function register_tasks(scheduler)
			print("[Lua] Registering tasks")
			
			local function task1()
				print("[Lua] Task 1 started")
				
				local results = exec_async(
					create_request("http", "get", "api1.example.com"),
					create_request("http", "get", "api2.example.com"),
					create_request("db", "query", "SELECT * FROM users")
				)
				
				print("[Lua] Task 1 got first batch results")
				
				results = exec_async(
					create_request("compute", "heavy_calc", 42),
					create_request("http", "post", "api3.example.com")
				)
				
				print("[Lua] Task 1 got second batch results")
				print("[Lua] Task 1 finished")
			end

			local function task2()
				results = exec_async(
					create_request("compute", "heavy_calc", 42),
					create_request("http", "post", "api3.example.com")
				)
				
				print("[Lua] Task 2 got second batch results")
				print("[Lua] Task 2 finished")
			end

			scheduler.createCoroutine(task1)
			scheduler.createCoroutine(task2) 
		end
	`); err != nil {
		panic(err)
	}

	registerTasks := L.GetGlobal("register_tasks").(*lua.LFunction)
	if err := L.CallByParam(lua.P{
		Fn:      registerTasks,
		NRet:    0,
		Protect: true,
	}, L.GetGlobal("scheduler")); err != nil {
		panic(err)
	}

	for {
		yieldedTasks := scheduler.GetYieldedTasks()
		if len(yieldedTasks) == 0 {
			log.Println("[Main] No more yielded tasks, exiting")
			break
		}

		for _, task := range yieldedTasks {
			if len(task.yieldedVals) < 1 {
				continue
			}

			cmd := task.yieldedVals[0]
			if cmdStr, ok := cmd.(lua.LString); ok {
				log.Printf("[Main] Processing command '%s'\n", cmdStr)

				switch cmdStr {
				case "batch":
					if len(task.yieldedVals) < 2 {
						log.Printf("[Main] Invalid batch request - missing values\n")
						continue
					}

					requestsTable, ok := task.yieldedVals[1].(*lua.LTable)
					if !ok {
						log.Printf("[Main] Invalid batch request - not a table\n")
						continue
					}

					results := L.NewTable()
					requestsTable.ForEach(func(_, value lua.LValue) {
						reqTable := value.(*lua.LTable)
						reqType := reqTable.RawGetString("type")
						reqParams := reqTable.RawGetString("params").(*lua.LTable)

						var result lua.LValue
						switch reqType.String() {
						case "http":
							result = lua.LString(fmt.Sprintf("http_response_%v", reqParams.RawGetInt(1)))
						case "db":
							result = lua.LString("db_result_data")
						case "compute":
							result = lua.LNumber(123)
						}
						results.Append(result)
					})

					task.resumeVal = results

					newlyYielded, err := scheduler.Step(task)
					if err != nil {
						log.Printf("[Main] Error stepping task: %v\n", err)
						scheduler.RemoveTask(task)
						continue
					}

					if task.state == lua.ResumeOK {
						log.Printf("[Main] Task completed\n")
						scheduler.RemoveTask(task)
					}

					// Process any newly yielded tasks immediately if needed
					_ = newlyYielded // In this example we'll process them in next loop iteration
				}
			}
		}
	}
}
