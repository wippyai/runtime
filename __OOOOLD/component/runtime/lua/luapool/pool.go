package pool

// tasks
// 1. Properly access to the context of the request (in the modules, like sql)
// 2. Concurrency
import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/runtime/lua"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/luapool/vm"
	"go.uber.org/zap"
)

type Config struct {
	// Number of Virtual Machines (Lua) to be created
	numVMs int
	// lua script code (actual lua code, tool)
	script string
	// mainFnName function name (entry point, might be any name, not only mainFnName)
	mainFnName string
}

func NewPoolCfg(numVMs int, script, mainFnName string) *Config {
	return &Config{
		numVMs:     numVMs,
		script:     script,
		mainFnName: mainFnName,
	}
}

type Task struct {
	args     any
	scriptID string
	resp     chan<- string
}

func NewPoolTask(scriptID string, args any) *Task {
	return &Task{
		args:     args,
		scriptID: scriptID,
	}
}

type Pool struct {
	taskQueue chan *Task
	stopCh    chan struct{}
	logger    *zap.Logger
	timeout   time.Duration
	modules   []lua.Module
	vms       map[string]chan *vm.VM
}

// scripts - scriptID -> script
// module - scriptID -> []Modules ?
func NewLuaPool(log *zap.Logger, scripts map[string]*Config, options ...Options) (*Pool, error) {
	lp := &Pool{}

	// apply options
	for _, opt := range options {
		opt(lp)
	}

	// AFTER INITIALIZATION THIS MAP IS READ-ONLY
	vms := make(map[string]chan *vm.VM)
	for scriptID, cfg := range scripts {
		log.Debug("creating vms pool", zap.String("scriptID", scriptID), zap.Int("number of VMs to create", cfg.numVMs))
		ch := make(chan *vm.VM, cfg.numVMs)
		for i := 0; i < cfg.numVMs; i++ {
			log.Debug("creating vm", zap.String("scriptID", scriptID), zap.String("script", cfg.script), zap.String("main", cfg.mainFnName))
			vm, err := vm.New(log, cfg.script, cfg.mainFnName, lp.modules...)
			if err != nil {
				return nil, err
			}

			// save vm
			// we have to use select to avoid blocking and inconsistency between number of vms and channel size
			select {
			case ch <- vm:
			default:
				log.Error("failed to save vm")
				return nil, errors.New("failed to save vm, critical error")
			}
		}

		vms[scriptID] = ch
	}

	if lp.timeout == 0 {
		lp.timeout = time.Minute * 30
	}

	lp.logger = log
	lp.vms = vms
	lp.stopCh = make(chan struct{})
	lp.taskQueue = make(chan *Task, 10)

	lp.poll()

	return lp, nil
}

// Queue adds task to the queue
func (w *Pool) Queue(task *Task) <-chan string {
	rc := make(chan string, 1)
	task.resp = rc
	w.taskQueue <- task
	return rc
}

// here is the actual work happens
func (w *Pool) do(v *vm.VM, task *Task) error {
	if task == nil {
		w.logger.Error("task is nil")
		return nil
	}

	w.logger.Info("executing script on VM", zap.Any("sid", task.scriptID), zap.Any("args", task.args))

	res, err := v.Execute(context.Background(), task.args)
	if err != nil {
		return err
	}

	select {
	case task.resp <- res:
		// send response
		w.logger.Info("work done, response was sent", zap.String("result", res))
	default:
		w.logger.Error("failed to send the response")
	}

	close(task.resp)

	return nil
}

// Stop sends stop signal to every worker in the pool
func (w *Pool) Stop() {
	w.stopCh <- struct{}{}
}

// private ---

func (w *Pool) poll() {
	go func() {
		for {
			select {
			case work := <-w.taskQueue:
				// this is actually slows down the process
				go func() {
					w.logger.Info("work received")
					// get vms
					select {
					case <-time.After(w.timeout):
						w.logger.Error("task timed out", zap.String("scriptID", work.scriptID))
						// get the VM to execute the script
						// THIS IS A READ-ONLY MAP
						close(work.resp)
					case vm := <-w.vms[work.scriptID]:
						// execute the script
						err := w.do(vm, work)
						if err != nil {
							w.logger.Error("failed to execute script", zap.Error(err))
						}
						// we're writing to the channel, not the map
						w.vms[work.scriptID] <- vm
					}
				}()
			case <-w.stopCh:
				w.logger.Info("luapool stopped")
			}
		}
	}()
}
